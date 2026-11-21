package common

import (
	"bytes"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/telemetry"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQuietStreamMetricsRecordTerminalOnce(t *testing.T) {
	r := telemetry.NewRegistry()
	app := httpbridge.NewApp()
	app.Use(r.Middleware())
	app.Post("/v1/messages", func(c fiber.Ctx) error {
		progress := NewStreamProgressLogger("Messages", time.Now(), false).WithMetricsContext(httpbridge.Request(c).Context())
		defer progress.FinishMetricsFallback()
		progress.Finish("completed")
		progress.Finish("error")
		return nil
	})
	httpbridge.Handler(app).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", nil))
	var out bytes.Buffer
	r.WritePrometheus(&out)
	s := out.String()
	if !strings.Contains(s, `yunqi_sse_terminals_total{endpoint="messages",outcome="completed"} 1`) {
		t.Fatal(s)
	}
	if strings.Contains(s, `outcome="error"`) || strings.Contains(s, `outcome="unknown"`) {
		t.Fatal("multiple final states", s)
	}
}
