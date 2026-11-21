package responses

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/autopilot"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/racing"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/types"
)

// Both racing attempts construct requests while the primary is waiting for SSE.
// Provider conversion state must belong to each attempt, including ordinary wires.
func TestResponsesRacingUsesIndependentProviderState(t *testing.T) {
	var primaryCalls, shadowCalls atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		select {
		case <-r.Context().Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
		_, sse := copilotEndpointFixture("responses")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse)
	}))
	defer primary.Close()
	shadow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shadowCalls.Add(1)
		_, sse := copilotEndpointFixture("openai")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse)
	}))
	defer shadow.Close()
	compatOn, compatOff := true, false
	cm := setupResponsesTestConfigManager(t, []config.UpstreamConfig{
		{ChannelUID: "ch_primary", Name: "primary", ServiceType: "responses", CodexToolCompat: &compatOn, BaseURL: primary.URL, APIKeys: []string{"fake-primary"}, Priority: 1, Status: "active"},
		{ChannelUID: "ch_shadow", Name: "shadow", ServiceType: "openai", CodexToolCompat: &compatOff, BaseURL: shadow.URL, APIKeys: []string{"fake-shadow"}, Priority: 2, Status: "active"},
	})
	if err := cm.SetRacingEnabled(true); err != nil {
		t.Fatal(err)
	}

	sched := scheduler.NewChannelScheduler(cm, metrics.NewMetricsManager(), metrics.NewMetricsManager(), metrics.NewMetricsManager(), metrics.NewMetricsManager(), metrics.NewMetricsManager(), session.NewTraceAffinityManager(), nil)
	hub := &common.RacingHub{Registry: racing.NewRegistry(), Sem: racing.NewSemaphore(2), CandidateProvider: func(model, kind string) []autopilot.RoutingCandidate {
		return []autopilot.RoutingCandidate{{ChannelUID: "ch_primary", ActualModel: model, Selected: true}, {ChannelUID: "ch_shadow", ActualModel: model, Selected: true}}
	}}
	hub.SetBehaviorOverrideForTest(func(string) racing.Behavior { return racing.Behavior{MaxShadows: 1, StreamFloorMs: 5} })
	common.SetRacingHub(hub)
	defer common.SetRacingHub(nil)
	writer := httptest.NewRecorder()
	ctx, _ := httpbridge.TestContext(writer)
	body := []byte(`{"model":"opaque-model","input":"hello","stream":true,"store":false}`)
	httpbridge.SetRequest(ctx, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body))))
	var request types.ResponsesRequest
	json.Unmarshal(body, &request)
	handleMultiChannel(ctx, &config.EnvConfig{}, cm, sched, session.NewSessionManager(time.Hour, 100, 100000), body, request, "fake-user", time.Now(), false)
	if primaryCalls.Load() != 1 || shadowCalls.Load() != 1 {
		t.Fatalf("racing did not exercise both request builders: primary=%d shadow=%d body=%s", primaryCalls.Load(), shadowCalls.Load(), writer.Body.String())
	}
	if !strings.Contains(writer.Body.String(), "hello") {
		t.Fatalf("racing response lost: %s", writer.Body.String())
	}
}
