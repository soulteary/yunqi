// Package telemetry provides bounded operational metrics without request bodies,
// keys, models, URLs, correlation IDs or any other unbounded label values.
package telemetry

import (
	"context"
	"fmt"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

var durationBounds = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 300, 600}

type requestSeries struct {
	count    uint64
	sum      float64
	buckets  []uint64
	canceled uint64
}
type Registry struct {
	events   map[string]uint64
	mu       sync.Mutex
	requests map[string]*requestSeries
	active   map[string]int64
}

func NewRegistry() *Registry {
	return &Registry{requests: make(map[string]*requestSeries), active: make(map[string]int64)}
}
func endpoint(method, path string) string {
	if method == http.MethodGet && strings.HasSuffix(path, "/v1/responses") {
		return "responses_ws"
	}
	if method != http.MethodPost {
		return ""
	}
	suffixes := []struct{ suffix, kind string }{{"/v1/messages", "messages"}, {"/v1/chat/completions", "chat"}, {"/v1/responses", "responses"}, {"/v1/responses/compact", "compact"}, {"/v1/images/generations", "images"}, {"/v1/images/edits", "images"}, {"/v1/images/variations", "images"}, {"/v1/embeddings", "vectors"}, {"/v1/quote", "quote"}}
	for _, entry := range suffixes {
		if strings.HasSuffix(path, entry.suffix) {
			return entry.kind
		}
	}
	if strings.Contains(path, "/v1beta/models/") && (strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent")) {
		return "gemini"
	}
	return ""
}
func statusClass(code int) string {
	if code >= 100 && code < 600 {
		return fmt.Sprintf("%dxx", code/100)
	}
	return "other"
}
func (r *Registry) Middleware() fiber.Handler {
	return func(c fiber.Ctx) (err error) {
		req := httpbridge.Request(c)
		kind := endpoint(req.Method, req.URL.Path)
		if kind == "" {
			return c.Next()
		}
		timingCtx, timeline := WithTiming(req.Context())
		defer timeline.Finish()
		httpbridge.SetWriteObserver(c, func() func() { return Phase(timingCtx, "downstream_write") })
		httpbridge.SetRequest(c, req.WithContext(context.WithValue(timingCtx, metricContextKey{}, requestMetricContext{r, kind})))
		start := time.Now()
		r.mu.Lock()
		r.active[kind]++
		r.mu.Unlock()
		defer func() {
			p := recover()
			code := httpbridge.Writer(c).Status()
			if p != nil || err != nil && code < 400 {
				code = 500
			}
			key := kind + "|" + statusClass(code)
			elapsed := time.Since(start).Seconds()
			r.mu.Lock()
			r.active[kind]--
			s := r.requests[key]
			if s == nil {
				s = &requestSeries{buckets: make([]uint64, len(durationBounds))}
				r.requests[key] = s
			}
			s.count++
			s.sum += elapsed
			if req.Context().Err() != nil {
				s.canceled++
			}
			for i, bound := range durationBounds {
				if elapsed <= bound {
					s.buckets[i]++
				}
			}
			r.mu.Unlock()
			if p != nil {
				panic(p)
			}
		}()
		return c.Next()
	}
}
func (r *Registry) Serve(c fiber.Ctx) error {
	w := httpbridge.Writer(c)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	r.WritePrometheus(w)
	return nil
}

// WritePrometheus follows the Prometheus 0.0.4 text exposition format. Metric
// families are grouped and histogram buckets are cumulative, including +Inf.
func (r *Registry) WritePrometheus(w io.Writer) {
	r.mu.Lock()
	requests := make(map[string]*requestSeries, len(r.requests))
	for key, series := range r.requests {
		copy := *series
		copy.buckets = append([]uint64(nil), series.buckets...)
		requests[key] = &copy
	}
	active := make(map[string]int64, len(r.active))
	for key, value := range r.active {
		active[key] = value
	}
	events := make(map[string]uint64, len(r.events))
	for key, value := range r.events {
		events[key] = value
	}
	r.mu.Unlock()
	keys := make([]string, 0, len(requests))
	for key := range requests {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	_, _ = fmt.Fprint(w, "# HELP yunqi_proxy_requests_total Completed proxy handlers, including failures.\n# TYPE yunqi_proxy_requests_total counter\n")
	for _, key := range keys {
		kind, class, _ := strings.Cut(key, "|")
		_, _ = fmt.Fprintf(w, "yunqi_proxy_requests_total{endpoint=%q,status_class=%q} %d\n", kind, class, requests[key].count)
	}
	_, _ = fmt.Fprint(w, "# HELP yunqi_proxy_request_duration_seconds Complete handler lifetime, including streaming.\n# TYPE yunqi_proxy_request_duration_seconds histogram\n")
	for _, key := range keys {
		kind, class, _ := strings.Cut(key, "|")
		s := requests[key]
		for i, bound := range durationBounds {
			_, _ = fmt.Fprintf(w, "yunqi_proxy_request_duration_seconds_bucket{endpoint=%q,status_class=%q,le=%q} %d\n", kind, class, fmt.Sprint(bound), s.buckets[i])
		}
		_, _ = fmt.Fprintf(w, "yunqi_proxy_request_duration_seconds_bucket{endpoint=%q,status_class=%q,le=\"+Inf\"} %d\nyunqi_proxy_request_duration_seconds_sum{endpoint=%q,status_class=%q} %g\nyunqi_proxy_request_duration_seconds_count{endpoint=%q,status_class=%q} %d\n", kind, class, s.count, kind, class, s.sum, kind, class, s.count)
	}
	_, _ = fmt.Fprint(w, "# HELP yunqi_proxy_cancellations_total Handlers whose request context was canceled.\n# TYPE yunqi_proxy_cancellations_total counter\n")
	for _, key := range keys {
		kind, class, _ := strings.Cut(key, "|")
		_, _ = fmt.Fprintf(w, "yunqi_proxy_cancellations_total{endpoint=%q,status_class=%q} %d\n", kind, class, requests[key].canceled)
	}
	_, _ = fmt.Fprint(w, "# HELP yunqi_proxy_inflight Current handlers, including open streams.\n# TYPE yunqi_proxy_inflight gauge\n")
	kinds := make([]string, 0, len(active))
	for kind := range active {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		_, _ = fmt.Fprintf(w, "yunqi_proxy_inflight{endpoint=%q} %d\n", kind, active[kind])
	}
	writeEvents(w, events)
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	_, _ = fmt.Fprintf(w, "# HELP yunqi_runtime_goroutines Current Go goroutines.\n# TYPE yunqi_runtime_goroutines gauge\nyunqi_runtime_goroutines %d\n# HELP yunqi_runtime_heap_alloc_bytes Current Go heap allocation.\n# TYPE yunqi_runtime_heap_alloc_bytes gauge\nyunqi_runtime_heap_alloc_bytes %d\n", runtime.NumGoroutine(), mem.HeapAlloc)
}
