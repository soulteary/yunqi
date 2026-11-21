package httpbridge_test

import (
	"context"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/telemetry"
	"net/http/httptest"
	"testing"
)

type blockedWrite struct {
	*httptest.ResponseRecorder
	entered, release chan struct{}
}

func (w *blockedWrite) Write(p []byte) (int, error) {
	close(w.entered)
	<-w.release
	return w.ResponseRecorder.Write(p)
}
func TestDownstreamTimingCoversActualWriterBlock(t *testing.T) {
	ctx, tl := telemetry.WithTiming(context.Background())
	app := httpbridge.NewApp()
	app.Post("/v1/messages", func(c fiber.Ctx) error {
		httpbridge.SetWriteObserver(c, func() func() { return telemetry.Phase(ctx, "downstream_write") })
		httpbridge.Text(c, 200, "ok")
		return nil
	})
	w := &blockedWrite{httptest.NewRecorder(), make(chan struct{}), make(chan struct{})}
	done := make(chan struct{})
	go func() {
		httpbridge.Handler(app).ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
		close(done)
	}()
	<-w.entered
	before := tl.Snapshot()
	if _, ok := before.StagesMs["downstream_write"]; !ok {
		close(w.release)
		<-done
		t.Fatal("writer was not observed")
	}
	close(w.release)
	<-done
	tl.Finish()
	after := tl.Snapshot()
	if after.StagesMs["downstream_write"] < before.StagesMs["downstream_write"] || after.StagesMs["downstream_write"] > after.TotalMs || w.Body.String() != "ok" {
		t.Fatalf("before=%+v after=%+v body=%s", before, after, w.Body.String())
	}
}
