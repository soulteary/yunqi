package telemetry

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"encoding/json"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/redaction"
)

func TestDiagnosticLifetimeRetentionPrivacyAndDetachedSnapshot(t *testing.T) {
	restore := redaction.Enable([]string{"private-credential"})
	defer restore()
	r := NewRegistry()
	app := httpbridge.NewApp()
	app.Use(r.Middleware())
	var saved context.Context
	app.Post("/v1/responses", func(c fiber.Ctx) error {
		saved = httpbridge.Request(c).Context()
		LinkInternal(saved, "srv_correlation", "rt_internal")
		d := DiagnosticsFromContext(saved)
		d.Attempt(DiagnosticAttempt{AttemptID: "a", ChannelUID: "ch1", KeyUID: "key1", Protocol: "Responses", Source: "proxy", Status: "connecting"})
		d.Attempt(DiagnosticAttempt{AttemptID: "a", ChannelUID: "ch1", KeyUID: "key1", Protocol: "Responses", Source: "proxy", Status: "failed", StatusCode: 429, ErrorSource: "upstream", UpstreamRequestID: "private-credential"})
		d.Attempt(DiagnosticAttempt{AttemptID: "b", ChannelUID: "ch2", KeyUID: "key2", Protocol: "Chat", Source: "proxy", Status: "completed", RacingRole: "shadow", RacingStatus: "won", UpstreamRequestID: "provider-123"})
		Event(saved, "sse_terminal", "failed")
		return nil
	})
	req := httptest.NewRequest("POST", "/v1/responses?key=private-credential", strings.NewReader(`{"model":"private-model","input":"private-prompt"}`))
	req.Header.Set("Authorization", "Bearer private-credential")
	w := httptest.NewRecorder()
	httpbridge.Handler(app).ServeHTTP(w, req)
	id := w.Header().Get("X-Yunqi-Request-Id")
	if id == "" || DiagnosticRequestID(saved) != id {
		t.Fatal("missing trusted request ID")
	}
	v, ok := r.LookupDiagnostic("srv_correlation")
	if !ok || v.ID != id || !v.Completed || len(v.Attempts) != 2 || v.Attempts[0].ErrorSource != "upstream" || v.Attempts[1].RacingStatus != "won" || v.TerminalOutcome != "failed" {
		t.Fatalf("bad logical request %+v", v)
	}
	if v.Attempts[1].Protocol != "chat" {
		t.Fatal("wire protocol label", v.Attempts)
	}
	raw, _ := json.Marshal(v)
	for _, secret := range []string{"private-credential", "private-model", "private-prompt", "Authorization", "baseUrl"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private value leaked", secret)
		}
	}
	v.Attempts[0].KeyUID = "mutated"
	v.Events[0].Stage = "mutated"
	v.PhaseTiming.StagesMs["queue"] = 999
	again, _ := r.LookupDiagnostic(id)
	if again.Attempts[0].KeyUID == "mutated" || again.Events[0].Stage == "mutated" || again.PhaseTiming.StagesMs["queue"] == 999 {
		t.Fatal("snapshot aliases retained state")
	}
	// A racing loser can complete after the parent HTTP request returned.
	DiagnosticsFromContext(saved).Attempt(DiagnosticAttempt{AttemptID: "c", Status: "racing_lost", RacingRole: "primary", RacingStatus: "lost", ErrorSource: "racing"})
	again, _ = r.LookupDiagnostic(id)
	if len(again.Attempts) != 3 {
		t.Fatal("late loser was lost")
	}
}
func TestDiagnosticBoundsAndConcurrentAttempts(t *testing.T) {
	r := NewRegistry()
	ctx, timing := WithTiming(context.Background())
	ctx, d := r.startDiagnostics(ctx, "responses", timing)
	var wg sync.WaitGroup
	for i := 0; i < 512; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("a%d", i)
			d.Attempt(DiagnosticAttempt{AttemptID: id, Status: "pending"})
			d.Attempt(DiagnosticAttempt{AttemptID: id, Status: "completed"})
			d.Event("upstream", "failed", id)
			_ = d.Snapshot()
		}(i)
	}
	wg.Wait()
	d.Finish(200, false)
	v := d.Snapshot()
	if len(v.Attempts) > MaxDiagnosticAttempts || len(v.Events) > MaxDiagnosticEvents || v.DroppedAttempts == 0 || v.DroppedEvents == 0 {
		t.Fatalf("unbounded diagnostics %+v", v)
	}
	old := DiagnosticRequestID(ctx)
	for i := 0; i < MaxDiagnosticRequests; i++ {
		r.startDiagnostics(context.Background(), "chat", nil)
	}
	if _, ok := r.LookupDiagnostic(old); ok {
		t.Fatal("old request not evicted")
	}
	r.mu.Lock()
	n := len(r.diagnostics)
	r.mu.Unlock()
	if n != MaxDiagnosticRequests {
		t.Fatal(n)
	}
}
func TestDiagnosticTurnContextIsIndependent(t *testing.T) {
	r := NewRegistry()
	parent, _ := r.startDiagnostics(context.Background(), "responses_ws", nil)
	one, d1 := r.startDiagnostics(parent, "responses", nil)
	two, d2 := r.startDiagnostics(parent, "responses", nil)
	d1.Event("request", "started", "")
	d2.Finish(200, true)
	if DiagnosticRequestID(one) == DiagnosticRequestID(two) || DiagnosticRequestID(one) == DiagnosticRequestID(parent) || len(d1.Snapshot().Events) != 2 || !d2.Snapshot().Canceled {
		t.Fatal("turn diagnostics shared")
	}
	if DiagnosticsFromContext(context.Background()) != nil {
		t.Fatal("unexpected global diagnostics")
	}

}
