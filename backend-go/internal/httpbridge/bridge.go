// Package httpbridge connects Fiber routing to the gateway's net/http transport.
// The original request and writer remain authoritative for cancellation, HTTP/2,
// streaming, multipart limits and WebSocket hijacking. It does not wrap Fiber.
package httpbridge

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/valyala/fasthttp"
)

type stateKey struct{}

var key = &stateKey{}

type requestState struct {
	request *http.Request
	writer  ResponseWriter
	mu      sync.RWMutex
	values  map[string]any
	params  map[string]string
	aborted bool
}

// NewApp uses Fiber for route matching, handler dispatch and middleware chaining.
// The production server must call Handler(app), so transport semantics stay intact.
func NewApp() *fiber.App {
	app := fiber.New(fiber.Config{
		CaseSensitive: true, StrictRouting: true, UnescapePath: true,
		DisablePreParseMultipartForm: true,
		ErrorHandler: func(c fiber.Ctx, err error) error {
			code := http.StatusInternalServerError
			var fe *fiber.Error
			if errors.As(err, &fe) {
				code = fe.Code
				if code == http.StatusMethodNotAllowed {
					code = http.StatusNotFound
				}
			}
			if !Writer(c).Written() {
				message := http.StatusText(code)
				if code == http.StatusNotFound {
					message = "404 page not found"
				}
				Text(c, code, "%s", message)
			}
			return nil
		},
	})
	app.Hooks().OnRoute(func(route fiber.Route) error {
		if route.Method == http.MethodGet {
			endpoint := false
			for _, registered := range app.GetRoutes(true) {
				if len(registered.Handlers) > 0 && len(route.Handlers) > 0 && &registered.Handlers[0] == &route.Handlers[0] {
					endpoint = true
					break
				}
			}
			if !endpoint {
				return nil
			}
			for i, h := range route.Handlers {
				handler := h
				route.Handlers[i] = func(c fiber.Ctx) error {
					// Fiber adds HEAD companions to GET. The previous gateway exposed HEAD
					// only where explicitly registered, so implicit HEAD uses the fallback.
					if Request(c).Method == http.MethodHead {
						return fallback(c)
					}
					return handler(c)
				}
			}
		}
		return nil
	})
	app.State().Set("ccx.httpbridge.handler", &handlerState{})
	app.Use(func(c fiber.Ctx) error {
		if s, ok := c.RequestCtx().UserValue(key).(*requestState); ok {
			c.SetContext(s.request.Context())
		}

		if strings.HasSuffix(c.Path(), "/") && c.Path() != "/" {
			candidate := strings.TrimSuffix(c.Path(), "/")
			for _, route := range c.App().GetRoutes(true) {
				if route.Method == Request(c).Method && fiber.RoutePatternMatch(candidate, route.Path, c.App().Config()) {
					target := candidate
					if query := Request(c).URL.RawQuery; query != "" {
						target += "?" + query
					}
					code := http.StatusTemporaryRedirect
					if Request(c).Method == http.MethodGet {
						code = http.StatusMovedPermanently
					}
					http.Redirect(Writer(c), Request(c), target, code)
					return nil
				}
			}
		}
		return c.Next()
	})
	return app
}

// Handler leaves request bodies streaming: handlers read the original body once,
// under the same MaxBytesReader and timeout policies as before the migration.
// Only routing metadata is copied to fasthttp; responses are written directly.
type handlerState struct {
	mu      sync.Mutex
	handler http.Handler
	routes  uint32
}

func Handler(app *fiber.App) http.Handler {
	entry := app.State().MustGet("ccx.httpbridge.handler").(*handlerState)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.handler == nil || entry.routes != app.HandlersCount() {
		prioritizeRoutes(app)
		entry.handler = transportHandler(app)
		entry.routes = app.HandlersCount()
	}
	return entry.handler
}

// Fiber matches in registration order. Keep static API paths ahead of parameter
// paths, matching the previous gateway surface, without moving middleware.
func prioritizeRoutes(app *fiber.App) {
	endpoints := make(map[*fiber.Handler]bool)
	for _, route := range app.GetRoutes(true) {
		if len(route.Handlers) > 0 {
			endpoints[&route.Handlers[0]] = true
		}
	}
	isEndpoint := func(route *fiber.Route) bool { return len(route.Handlers) > 0 && endpoints[&route.Handlers[0]] }

	for _, stack := range app.Stack() {
		for i := 0; i < len(stack); {
			if !isEndpoint(stack[i]) {
				i++
				continue
			}
			j := i
			for j < len(stack) && isEndpoint(stack[j]) {
				j++
			}
			block := stack[i:j]
			sort.SliceStable(block, func(a, b int) bool { return routeLess(block[a].Path, block[b].Path) })
			i = j
		}
	}
}
func routeLess(a, b string) bool {
	left, right := strings.Split(a, "/"), strings.Split(b, "/")
	rank := func(v string) int {
		if strings.HasPrefix(v, "*") || strings.HasPrefix(v, "+") {
			return 2
		}
		if strings.HasPrefix(v, ":") {
			return 1
		}
		return 0
	}
	for i := 0; i < len(left) && i < len(right); i++ {
		l, r := rank(left[i]), rank(right[i])
		if l != r {
			return l < r
		}
		if l == 0 && left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return len(left) > len(right)
}
func transportHandler(app *fiber.App) http.Handler {
	handler := app.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var fc fasthttp.RequestCtx
		fc.Init(&fasthttp.Request{}, nil, nil)
		fc.Request.SetRequestURI(r.URL.RequestURI())
		fc.Request.Header.SetMethod(r.Method)
		fc.Request.Header.SetHost(r.Host)
		proto := r.Proto
		if r.ProtoMajor == 2 {
			proto = "HTTP/2"
		}
		if r.ProtoMajor == 3 {
			proto = "HTTP/3"
		}
		if proto != "" {
			fc.Request.Header.SetProtocol(proto)
		}
		for k, vs := range r.Header {
			for _, v := range vs {
				fc.Request.Header.Add(k, v)
			}
		}
		s := &requestState{request: r, values: make(map[string]any)}
		rw := &responseWriter{ResponseWriter: w, status: 200, size: -1, request: r, header: &fc.Response.Header}
		s.writer = rw
		fc.SetUserValue(key, s)
		handler(&fc)
		if !rw.Written() && !rw.hijacked {
			if fc.Response.StatusCode() != 200 || rw.status == 200 {
				rw.WriteHeader(fc.Response.StatusCode())
			}
			rw.WriteHeaderNow()
			if r.Method != http.MethodHead {
				_, _ = rw.Write(fc.Response.Body())
			}
		}
	})
}

func state(c fiber.Ctx) *requestState {
	if s, ok := c.RequestCtx().UserValue(key).(*requestState); ok {
		return s
	}
	// This path supports native Fiber app.Test and direct context unit tests.
	req, err := adaptor.ConvertRequest(c, true)
	if err != nil {
		req = &http.Request{Method: c.Method(), URL: &url.URL{Path: c.Path()}, Header: make(http.Header), Body: http.NoBody}
	}
	req = req.WithContext(c.Context())
	s := &requestState{request: req, values: make(map[string]any)}
	s.writer = &responseWriter{ResponseWriter: &fiberResponseWriter{ctx: c}, status: 200, size: -1, request: req}
	c.RequestCtx().SetUserValue(key, s)
	return s
}

func Request(c fiber.Ctx) *http.Request       { return state(c).request }
func SetRequest(c fiber.Ctx, r *http.Request) { state(c).request = r; c.SetContext(r.Context()) }
func Writer(c fiber.Ctx) ResponseWriter       { return state(c).writer }
func SetWriter(c fiber.Ctx, w ResponseWriter) { state(c).writer = w }
func SetValue(c fiber.Ctx, k string, v any) {
	s := state(c)
	s.mu.Lock()
	s.values[k] = v
	s.mu.Unlock()
}
func Value(c fiber.Ctx, k string) (any, bool) {
	s := state(c)
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.values[k]
	return v, ok
}
func StringValue(c fiber.Ctx, k string) string { v, _ := Value(c, k); s, _ := v.(string); return s }
func Abort(c fiber.Ctx)                        { state(c).aborted = true }
func IsAborted(c fiber.Ctx) bool               { return state(c).aborted }
func AbortWithStatus(c fiber.Ctx, code int)    { Status(c, code); Abort(c); Writer(c).WriteHeaderNow() }
func Status(c fiber.Ctx, code int)             { c.Status(code); Writer(c).WriteHeader(code) }
func Header(c fiber.Ctx, k, v string) {
	if v == "" {
		c.Response().Header.Del(k)
		Writer(c).Header().Del(k)
	} else {
		c.Set(k, v)
		Writer(c).Header().Set(k, v)
	}
}

func JSON(c fiber.Ctx, code int, value any) {
	if err := c.Status(code).JSON(value); err != nil {
		Text(c, 500, "%s", http.StatusText(500))
		return
	}
	if Writer(c).Header().Get("Content-Type") == "" {
		Writer(c).Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	Writer(c).WriteHeader(code)
	body := append([]byte(nil), c.Response().Body()...)
	c.Response().ResetBody()
	_, _ = Writer(c).Write(body)
}
func Data(c fiber.Ctx, code int, contentType string, body []byte) {
	Header(c, "Content-Type", contentType)
	Status(c, code)
	_, _ = Writer(c).Write(body)
}
func Text(c fiber.Ctx, code int, format string, args ...any) {
	Data(c, code, "text/plain; charset=utf-8", []byte(fmt.Sprintf(format, args...)))
}

var validate = func() *validator.Validate { v := validator.New(); v.SetTagName("binding"); return v }()

func BindJSON(c fiber.Ctx, value any) error {
	if Request(c).Body == nil {
		return io.EOF
	}
	if err := json.NewDecoder(Request(c).Body).Decode(value); err != nil {
		return err
	}
	v := reflect.ValueOf(value)
	for v.IsValid() && v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.IsValid() && v.Kind() == reflect.Struct {
		return validate.StructCtx(Request(c).Context(), value)
	}
	return nil
}

type ParamValue struct{ Key, Value string }
type Params []ParamValue

func SetParams(c fiber.Ctx, p Params) {
	s := state(c)
	s.params = make(map[string]string, len(p))
	for _, v := range p {
		s.params[v.Key] = v.Value
	}
}
func Param(c fiber.Ctx, name string) string {
	if v, ok := state(c).params[name]; ok {
		return v
	}
	if c.Route() == nil {
		return ""
	}
	switch name {
	case "rest", "modelAction", "filepath":
		return "/" + c.Params("*")
	}
	// Fiber parameters alias pooled request buffers. Callers may retain IDs or
	// keys in configuration after this request returns.
	return strings.Clone(c.Params(name))
}
func ClientIP(c fiber.Ctx) string {
	// Preserve the existing reverse-proxy interpretation; do not newly trust a
	// different forwarding header simply because the routing framework changed.
	for _, h := range []string{"X-Forwarded-For", "X-Real-IP"} {
		parts := strings.Split(Request(c).Header.Get(h), ",")
		valid := true
		for _, p := range parts {
			if net.ParseIP(strings.TrimSpace(p)) == nil {
				valid = false
				break
			}
		}
		if valid && len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(Request(c).RemoteAddr)
	if err == nil {
		return host
	}
	return Request(c).RemoteAddr
}

// Copy allocates an independent Fiber context for an asynchronous proxy branch.
// Fiber contexts are pooled; the source context must never be retained by a branch.
func Copy(c fiber.Ctx) fiber.Ctx {
	fc := new(fasthttp.RequestCtx)
	fc.Init(&fasthttp.Request{}, nil, nil)
	c.Request().CopyTo(&fc.Request)
	copyCtx := c.App().AcquireCtx(fc)
	original := state(c)
	original.mu.RLock()
	next := &requestState{request: original.request, writer: original.writer, values: make(map[string]any), params: make(map[string]string)}
	for k, v := range original.values {
		next.values[k] = v
	}
	for k, v := range original.params {
		next.params[k] = v
	}
	original.mu.RUnlock()
	if route := c.Route(); route != nil {
		for _, name := range route.Params {
			next.params[name] = c.Params(name)
		}
	}
	for _, name := range []string{"id", "routePrefix", "model", "modelAction", "rest", "filepath"} {
		next.params[name] = Param(c, name)
	}
	fc.SetUserValue(key, next)
	copyCtx.SetContext(c.Context())
	return copyCtx
}
func CopyValues(dst, src fiber.Ctx, skipPrefix string) {
	s := state(src)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for k, v := range s.values {
		if !strings.HasPrefix(k, skipPrefix) {
			SetValue(dst, k, v)
		}
	}
}

// NewContext is used by in-process discovery and capability requests as well as
// tests, so these requests execute exactly the same Fiber handlers as HTTP traffic.
func NewContext(w http.ResponseWriter, r *http.Request) (fiber.Ctx, *fiber.App) {
	app := NewApp()
	fc := new(fasthttp.RequestCtx)
	fc.Init(&fasthttp.Request{}, nil, nil)
	fc.Request.Header.SetMethod(r.Method)
	fc.Request.SetRequestURI(r.URL.RequestURI())
	for k, vs := range r.Header {
		for _, v := range vs {
			fc.Request.Header.Add(k, v)
		}
	}
	c := app.AcquireCtx(fc)
	s := &requestState{request: r, values: make(map[string]any)}
	s.writer = &responseWriter{ResponseWriter: w, status: 200, size: -1, request: r, header: &fc.Response.Header}
	fc.SetUserValue(key, s)
	c.SetContext(r.Context())
	return c, app
}
func TestContext(w http.ResponseWriter) (fiber.Ctx, *fiber.App) {
	r, _ := http.NewRequest(http.MethodGet, "/", http.NoBody)
	return NewContext(w, r)
}

// ResponseWriter describes only the net/http capabilities used by the proxy.
type ResponseWriter interface {
	http.ResponseWriter
	http.Flusher
	http.Hijacker
	http.CloseNotifier
	WriteString(string) (int, error)
	WriteHeaderNow()
	Status() int
	Size() int
	Written() bool
	Pusher() http.Pusher
}
type responseWriter struct {
	http.ResponseWriter
	status, size int
	request      *http.Request
	header       *fasthttp.ResponseHeader
	hijacked     bool
}

func (w *responseWriter) WriteHeader(code int) {
	if !w.Written() && code > 0 {
		w.status = code
	}
}
func (w *responseWriter) WriteHeaderNow() {
	if w.Written() || w.hijacked {
		return
	}
	if w.header != nil {
		w.header.VisitAll(func(k, v []byte) {
			if w.Header().Get(string(k)) == "" {
				w.Header().Add(string(k), string(v))
			}
		})
	}
	w.size = 0
	w.ResponseWriter.WriteHeader(w.status)
}
func (w *responseWriter) Write(p []byte) (int, error) {
	w.WriteHeaderNow()
	n, e := w.ResponseWriter.Write(p)
	w.size += n
	return n, e
}
func (w *responseWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *responseWriter) Status() int                       { return w.status }
func (w *responseWriter) Size() int                         { return w.size }
func (w *responseWriter) Written() bool                     { return w.size >= 0 }
func (w *responseWriter) Flush() {
	w.WriteHeaderNow()
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.hijacked = true
	}
	return conn, rw, err
}
func (w *responseWriter) CloseNotify() <-chan bool {
	if n, ok := w.ResponseWriter.(http.CloseNotifier); ok {
		return n.CloseNotify()
	}
	ch := make(chan bool, 1)
	go func() { <-w.request.Context().Done(); ch <- true }()
	return ch
}
func (w *responseWriter) Pusher() http.Pusher         { p, _ := w.ResponseWriter.(http.Pusher); return p }
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type fiberResponseWriter struct {
	ctx    fiber.Ctx
	header http.Header
}

func (w *fiberResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *fiberResponseWriter) WriteHeader(code int) {
	for k, vs := range w.header {
		w.ctx.Response().Header.Del(k)
		for _, v := range vs {
			w.ctx.Response().Header.Add(k, v)
		}
	}
	w.ctx.Status(code)
}
func (w *fiberResponseWriter) Write(p []byte) (int, error) {
	w.ctx.Response().AppendBody(p)
	return len(p), nil
}

func GetHeader(c fiber.Ctx, name string) string { return Request(c).Header.Get(name) }
func Query(c fiber.Ctx, name string, defaults ...string) string {
	v, ok := Request(c).URL.Query()[name]
	if ok && len(v) > 0 {
		return v[0]
	}
	if len(defaults) > 0 {
		return defaults[0]
	}
	return ""
}

func ReadBody(c fiber.Ctx) ([]byte, error) { return io.ReadAll(Request(c).Body) }

// SetFallback supplies the SPA/API fallback used by unknown routes and implicit HEAD.
func SetFallback(app *fiber.App, handler fiber.Handler) {
	app.State().Set("ccx.httpbridge.fallback", handler)
}
func fallback(c fiber.Ctx) error {
	if h, ok := c.App().State().Get("ccx.httpbridge.fallback"); ok {
		return h.(fiber.Handler)(c)
	}
	Text(c, http.StatusNotFound, "404 page not found")
	return nil
}
