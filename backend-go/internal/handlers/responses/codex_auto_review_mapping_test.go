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

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/session"
)

func TestCodexAutoReviewMappingThroughResponsesHandler(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, tc := range []struct {
			name, requested, expected string
			enabled                   bool
		}{
			{"enabled", "codex-auto-review", "review-model", true},
			{"disabled", "codex-auto-review", "codex-auto-review", false},
			{"other_model", "normal-model", "normal-model", true},
		} {
			t.Run(fmt.Sprintf("%s/stream_%t", tc.name, stream), func(t *testing.T) {
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					var request map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					if r.URL.Path != "/v1/responses" || request["model"] != tc.expected || request["instructions"] != "Return the actual classification, never fabricate approval." {
						t.Errorf("mapping damaged request: %s %#v", r.URL.Path, request)
					}
					input, _ := json.Marshal(request["input"])
					if !strings.Contains(string(input), "Keep this developer instruction.") {
						t.Errorf("instruction lost: %s", input)
					}
					response := map[string]interface{}{"id": "resp_review", "object": "response", "status": "completed", "model": tc.expected, "output": []interface{}{map[string]interface{}{"id": "msg_review", "type": "message", "role": "assistant", "status": "completed", "content": []interface{}{map[string]interface{}{"type": "output_text", "text": "review result"}}}}, "usage": map[string]interface{}{"input_tokens": 11, "output_tokens": 5, "total_tokens": 16}}
					if !stream {
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(response)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"review result\"}\n\n")
					data, _ := json.Marshal(map[string]interface{}{"type": "response.completed", "response": response})
					fmt.Fprintf(w, "event: response.completed\ndata: %s\n\n", data)
				}))
				defer upstream.Close()
				manager := setupResponsesTestConfigManager(t, []config.UpstreamConfig{{Name: "review", BaseURL: upstream.URL, APIKeys: []string{"test-review-key"}, ServiceType: "responses", Status: "active", SupportedModels: []string{"review-model", "normal-model", "codex-auto-review"}}})
				if err := manager.SetCodexAutoReviewMapping(config.CodexAutoReviewMapping{Enabled: tc.enabled, TargetModel: "review-model"}); err != nil {
					t.Fatal(err)
				}
				sched := scheduler.NewChannelScheduler(manager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), metrics.NewMetricsManager(), metrics.NewMetricsManager(), metrics.NewMetricsManager(), session.NewTraceAffinityManager(), nil)
				router := httpbridge.NewApp()
				router.Post("/v1/responses", Handler(&config.EnvConfig{ProxyAccessKey: "secret-key", MaxRequestBodySize: 1024 * 1024}, manager, session.NewSessionManager(time.Hour, 100, 100000), sched))
				body := fmt.Sprintf(`{"model":%q,"stream":%t,"store":false,"instructions":"Return the actual classification, never fabricate approval.","input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"Keep this developer instruction."}]},{"type":"message","role":"user","content":"Review this action"}]}`, tc.requested, stream)
				result := performResponsesHandlerRequest(t, router, body)
				if result.Code != 200 || !strings.Contains(result.Body.String(), "review result") || calls.Load() != 1 {
					t.Fatalf("status=%d calls=%d body=%s", result.Code, calls.Load(), result.Body.String())
				}
			})
		}
	}
}

func TestCodexAutoReviewMappingChecksTargetCapacity(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Error("over-capacity request reached upstream")
		w.WriteHeader(500)
	}))
	defer upstream.Close()
	manager := setupResponsesTestConfigManager(t, []config.UpstreamConfig{{Name: "small", BaseURL: upstream.URL, APIKeys: []string{"fake-key"}, ServiceType: "responses", Status: "active", ModelCapabilities: map[string]config.UpstreamModelCapability{"small-target": {ContextWindowTokens: 64, MaxOutputTokens: 16}}}})
	if err := manager.SetCodexAutoReviewMapping(config.CodexAutoReviewMapping{Enabled: true, TargetModel: "small-target"}); err != nil {
		t.Fatal(err)
	}
	sched := scheduler.NewChannelScheduler(manager, metrics.NewMetricsManager(), metrics.NewMetricsManager(), metrics.NewMetricsManager(), metrics.NewMetricsManager(), metrics.NewMetricsManager(), session.NewTraceAffinityManager(), nil)
	router := httpbridge.NewApp()
	router.Post("/v1/responses", Handler(&config.EnvConfig{ProxyAccessKey: "secret-key", MaxRequestBodySize: 1024 * 1024}, manager, session.NewSessionManager(time.Hour, 100, 100000), sched))
	body, _ := json.Marshal(map[string]interface{}{"model": "codex-auto-review", "input": strings.Repeat("large input ", 1000), "max_output_tokens": 8})
	response := performResponsesHandlerRequest(t, router, string(body))
	if response.Code != 400 || calls.Load() != 0 || !strings.Contains(response.Body.String(), "small-target") {
		t.Fatalf("target capability bypassed: status=%d calls=%d body=%s", response.Code, calls.Load(), response.Body.String())
	}
}
