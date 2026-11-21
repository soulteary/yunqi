package telemetry

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestMetricsRemainBoundedAndExcludePrivateValues(t *testing.T) {
	r := NewRegistry()
	app := httpbridge.NewApp()
	app.Use(r.Middleware())
	app.Post("/:prefix/v1/responses", func(c fiber.Ctx) error { httpbridge.Text(c, 429, "secret-model"); return nil })
	for i := 0; i < 500; i++ {
		req := httptest.NewRequest("POST", fmt.Sprintf("/private-%d/v1/responses?key=secret-key", i), strings.NewReader(`{"model":"private-model"}`))
		req.Header.Set("Authorization", "Bearer secret-key")
		w := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(w, req)
		if w.Code != 429 {
			t.Fatal(w.Code)
		}
	}
	var out bytes.Buffer
	r.WritePrometheus(&out)
	text := out.String()
	for _, secret := range []string{"private-", "secret-", "model=", "key=", "url=", "request_id="} {
		if strings.Contains(text, secret) {
			t.Fatal("sensitive/unbounded label:", secret)
		}
	}
	if !strings.Contains(text, `yunqi_proxy_requests_total{endpoint="responses",status_class="4xx"} 500`) {
		t.Fatal(text)
	}
	if !strings.Contains(text, `le="+Inf"} 500`) {
		t.Fatal("histogram missing +Inf count")
	}
	r.mu.Lock()
	n := len(r.requests)
	r.mu.Unlock()
	if n != 1 {
		t.Fatal("cardinality", n)
	}
}
func TestMetricsTrackStreamingLifetimeAndCancellation(t *testing.T) {
	r := NewRegistry()
	app := httpbridge.NewApp()
	app.Use(r.Middleware())
	entered, release := make(chan struct{}), make(chan struct{})
	app.Post("/v1/messages", func(c fiber.Ctx) error { close(entered); <-release; httpbridge.Text(c, 200, "ok"); return nil })
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("POST", "/v1/messages", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { httpbridge.Handler(app).ServeHTTP(httptest.NewRecorder(), req); close(done) }()
	<-entered
	var out bytes.Buffer
	r.WritePrometheus(&out)
	if !strings.Contains(out.String(), `yunqi_proxy_inflight{endpoint="messages"} 1`) {
		t.Fatal(out.String())
	}
	cancel()
	close(release)
	<-done
	out.Reset()
	r.WritePrometheus(&out)
	if !strings.Contains(out.String(), `yunqi_proxy_cancellations_total{endpoint="messages",status_class="2xx"} 1`) {
		t.Fatal(out.String())
	}
	if !strings.Contains(out.String(), `yunqi_proxy_inflight{endpoint="messages"} 0`) {
		t.Fatal(out.String())
	}
}

type blockingMetricWriter struct {
	once             sync.Once
	entered, release chan struct{}
}

func (w *blockingMetricWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered); <-w.release })
	return len(p), nil
}
func TestSlowScrapeDoesNotBlockRequestMetrics(t *testing.T) {
	r := NewRegistry()
	w := &blockingMetricWriter{entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() { r.WritePrometheus(w); close(done) }()
	<-w.entered
	// Acquiring the metrics lock must work while the remote scrape is blocked.
	if !r.mu.TryLock() {
		close(w.release)
		<-done
		t.Fatal("slow scrape holds request lock")
	}
	r.mu.Unlock()
	close(w.release)
	<-done
}
