package httpbridge

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gorilla/websocket"
)

func TestStreamingFlushAndClientCancellation(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("http2=%v", http2), func(t *testing.T) {
			app := NewApp()
			cancelled := make(chan struct{})
			app.Get("/stream", func(c fiber.Ctx) error {
				Header(c, "Content-Type", "text/event-stream")
				_, err := Writer(c).WriteString("data: first\n\n")
				if err != nil {
					return err
				}
				Writer(c).Flush()
				<-Request(c).Context().Done()
				close(cancelled)
				return nil
			})
			server := httptest.NewUnstartedServer(Handler(app))
			server.EnableHTTP2 = http2
			if http2 {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			request, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/stream", nil)
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			if http2 && response.ProtoMajor != 2 {
				t.Fatalf("expected HTTP/2, got %s", response.Proto)
			}
			line, err := bufio.NewReader(response.Body).ReadString('\n')
			if err != nil || line != "data: first\n" {
				t.Fatalf("first frame before completion: %q %v", line, err)
			}
			_ = response.Body.Close()
			cancel()
			select {
			case <-cancelled:
			case <-time.After(2 * time.Second):
				t.Fatal("disconnect did not cancel handler")
			}
		})
	}
}

func TestWebSocketHijackRemainsUsable(t *testing.T) {
	app := NewApp()
	app.Get("/ws", func(c fiber.Ctx) error {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(Writer(c), Request(c), nil)
		if err != nil {
			return err
		}
		defer conn.Close()
		kind, body, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		return conn.WriteMessage(kind, body)
	})
	server := httptest.NewServer(Handler(app))
	defer server.Close()
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if resp.StatusCode != 101 {
		t.Fatal(resp.StatusCode)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("echo")); err != nil {
		t.Fatal(err)
	}
	_, body, err := conn.ReadMessage()
	if err != nil || string(body) != "echo" {
		t.Fatalf("echo: %q %v", body, err)
	}
}

func TestLargeBodyIsNotBufferedBeforeHandler(t *testing.T) {
	app := NewApp()
	read := 0
	source := &countedReader{Reader: strings.NewReader(strings.Repeat("x", 8<<20)), read: &read}
	app.Post("/upload", func(c fiber.Ctx) error {
		if read != 0 {
			t.Errorf("transport buffered %d bytes before handler", read)
		}
		n, err := io.Copy(io.Discard, Request(c).Body)
		if err != nil {
			return err
		}
		JSON(c, 200, fiber.Map{"bytes": n})
		return nil
	})
	recorder := httptest.NewRecorder()
	Handler(app).ServeHTTP(recorder, httptest.NewRequest("POST", "/upload", source))
	if recorder.Code != 200 || read != 8<<20 {
		t.Fatalf("status=%d read=%d body=%s", recorder.Code, read, recorder.Body.String())
	}
}

type countedReader struct {
	io.Reader
	read *int
}

func (r *countedReader) Read(p []byte) (int, error) {
	n, e := r.Reader.Read(p)
	*r.read += n
	return n, e
}

func TestBindingQueryAndHeaderCompatibility(t *testing.T) {
	c, _ := TestContext(httptest.NewRecorder())
	req := httptest.NewRequest("POST", "/test?empty=&value=first&value=second", strings.NewReader(`{"name":"ok"}`))
	req.Header.Set("X-Test", "fixture")
	SetRequest(c, req)
	var payload struct {
		Name string `json:"name" binding:"required"`
	}
	if err := BindJSON(c, &payload); err != nil || payload.Name != "ok" {
		t.Fatalf("binding: %+v %v", payload, err)
	}
	if GetHeader(c, "x-test") != "fixture" || Query(c, "value") != "first" || Query(c, "empty", "default") != "" || Query(c, "missing", "default") != "default" {
		t.Fatal("header/query semantics changed")
	}
	SetRequest(c, httptest.NewRequest("POST", "/test", strings.NewReader(`{}`)))
	if err := BindJSON(c, &struct {
		Name string `json:"name" binding:"required"`
	}{}); err == nil {
		t.Fatal("required field was not validated")
	}
}

func TestCopiedBranchStateIsIndependentAndRaceSafe(t *testing.T) {
	c, _ := TestContext(httptest.NewRecorder())
	SetParams(c, Params{{Key: "routePrefix", Value: "team"}})
	SetValue(c, "initial", "original")
	branch := Copy(c)
	SetValue(branch, "initial", "branch")
	if got, _ := Value(c, "initial"); got != "original" {
		t.Fatal("copy shares values")
	}
	if Param(branch, "routePrefix") != "team" {
		t.Fatal("copy lost parameters")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				SetValue(c, "counter", j)
				Value(c, "counter")
			}
		}()
	}
	wg.Wait()
	CopyValues(c, branch, "internal.")
	if got, _ := Value(c, "initial"); got != "branch" {
		t.Fatal("branch result not copied")
	}
}

func TestNativeMiddlewareAndHTTPStatus(t *testing.T) {
	app := NewApp()
	called := false
	app.Use(func(c fiber.Ctx) error {
		if GetHeader(c, "X-Key") != "ok" {
			JSON(c, 401, fiber.Map{"error": "unauthorized"})
			Abort(c)
			return nil
		}
		return c.Next()
	})
	app.Get("/guarded", func(c fiber.Ctx) error { called = true; Status(c, 204); return nil })
	denied := httptest.NewRecorder()
	Handler(app).ServeHTTP(denied, httptest.NewRequest("GET", "/guarded", nil))
	if denied.Code != 401 || called {
		t.Fatalf("denied status=%d called=%v", denied.Code, called)
	}
	allowed := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/guarded", nil)
	req.Header.Set("X-Key", "ok")
	Handler(app).ServeHTTP(allowed, req)
	if allowed.Code != 204 || !called {
		t.Fatalf("allowed status=%d called=%v", allowed.Code, called)
	}
}

func TestStaticRoutePriorityAndWildcardParameters(t *testing.T) {
	app := NewApp()
	app.Use(func(c fiber.Ctx) error { return c.Next() })
	app.Get("/api/channels/:id", func(c fiber.Ctx) error { Text(c, 200, "dynamic"); return nil })
	app.Get("/api/channels/export", func(c fiber.Ctx) error { Text(c, 200, "export"); return nil })
	app.Post("/:routePrefix/v1beta/models/*", func(c fiber.Ctx) error {
		JSON(c, 200, fiber.Map{"prefix": Param(c, "routePrefix"), "action": Param(c, "modelAction")})
		return nil
	})
	app.Use(func(c fiber.Ctx) error { Text(c, 404, "fallback"); return nil })
	recorder := httptest.NewRecorder()
	Handler(app).ServeHTTP(recorder, httptest.NewRequest("GET", "/api/channels/export", nil))
	if recorder.Body.String() != "export" {
		t.Fatalf("static route lost priority: %s", recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	Handler(app).ServeHTTP(recorder, httptest.NewRequest("POST", "/team/v1beta/models/gemini:generateContent", nil))
	if !strings.Contains(recorder.Body.String(), `"action":"/gemini:generateContent"`) {
		t.Fatal(recorder.Body.String())
	}
}

func TestNativeFiberTestDoesNotDuplicateJSON(t *testing.T) {
	app := NewApp()
	app.Get("/json", func(c fiber.Ctx) error { JSON(c, 200, fiber.Map{"ok": true}); return nil })
	resp, err := app.Test(httptest.NewRequest("GET", "/json", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != `{"ok":true}` {
		t.Fatalf("body=%s err=%v", body, err)
	}
}

func TestTrailingSlashRedirectAndHeadCompatibility(t *testing.T) {
	app := NewApp()
	app.Get("/api/test", func(c fiber.Ctx) error { JSON(c, 200, fiber.Map{"ok": true}); return nil })
	app.Post("/api/test", func(c fiber.Ctx) error { Status(c, 204); return nil })
	app.Head("/asset", func(c fiber.Ctx) error { Status(c, 204); return nil })
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/api/test/?a=b", 301}, {"POST", "/api/test/", 307}, {"HEAD", "/api/test", 404}, {"HEAD", "/asset", 204}} {
		w := httptest.NewRecorder()
		Handler(app).ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Errorf("%s %s = %d, want %d; %s", tc.method, tc.path, w.Code, tc.status, w.Body.String())
		}
		if tc.status == 301 && w.Header().Get("Location") != "/api/test?a=b" {
			t.Fatal(w.Header())
		}
	}
}
