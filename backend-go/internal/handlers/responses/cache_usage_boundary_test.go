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
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/types"
)

func TestResponsesCachedUsageClientBoundary(t *testing.T) {
	for _, sample := range []struct {
		name                         string
		input, cached, output, total int
	}{
		{"issue_789", 10773, 9984, 154, 10927},
		{"issue_52", 10932, 10880, 56, 10988},
		{"no_cache", 10773, 0, 154, 10927},
		{"fully_cached", 9984, 9984, 154, 10138},
		{"one_new_token", 9985, 9984, 154, 10139},
	} {
		for _, wire := range []string{"responses", "openai", "claude"} {
			for _, mode := range []string{"json", "stream", "sse_bridge"} {
				t.Run(sample.name+"/"+wire+"/"+mode, func(t *testing.T) {
					var calls atomic.Int32
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						var request map[string]interface{}
						if json.NewDecoder(r.Body).Decode(&request) != nil || request["model"] != "cache-model" {
							t.Error("invalid upstream request")
							http.Error(w, "invalid request", 400)
							return
						}
						wantPath := map[string]string{"responses": "/v1/responses", "openai": "/v1/chat/completions", "claude": "/v1/messages"}[wire]
						if r.URL.Path != wantPath {
							t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
						}
						message := map[string]interface{}{"id": "msg_cache", "type": "message", "role": "assistant", "status": "completed", "content": []interface{}{map[string]interface{}{"type": "output_text", "text": "cache result"}}}
						nativeUsage := map[string]interface{}{"input_tokens": sample.input, "output_tokens": sample.output, "total_tokens": sample.total, "input_tokens_details": map[string]interface{}{"cached_tokens": sample.cached}}
						chatUsage := map[string]interface{}{"prompt_tokens": sample.input, "completion_tokens": sample.output, "total_tokens": sample.total, "prompt_tokens_details": map[string]interface{}{"cached_tokens": sample.cached}}
						claudeUsage := map[string]interface{}{"input_tokens": sample.input - sample.cached, "output_tokens": sample.output, "cache_read_input_tokens": sample.cached}
						if mode == "json" {
							w.Header().Set("Content-Type", "application/json")
							var response interface{}
							switch wire {
							case "responses":
								response = map[string]interface{}{"id": "resp_cache", "model": "cache-model", "status": "completed", "output": []interface{}{message}, "usage": nativeUsage}
							case "openai":
								response = map[string]interface{}{"id": "chat_cache", "model": "cache-model", "choices": []interface{}{map[string]interface{}{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "cache result"}, "finish_reason": "stop"}}, "usage": chatUsage}
							case "claude":
								response = map[string]interface{}{"id": "msg_cache", "model": "cache-model", "type": "message", "role": "assistant", "content": []interface{}{map[string]interface{}{"type": "text", "text": "cache result"}}, "stop_reason": "end_turn", "usage": claudeUsage}
							}
							json.NewEncoder(w).Encode(response)
							return
						}
						w.Header().Set("Content-Type", "text/event-stream")
						emit := func(kind string, payload interface{}) {
							data, _ := json.Marshal(payload)
							if kind != "" {
								fmt.Fprintf(w, "event: %s\n", kind)
							}
							fmt.Fprintf(w, "data: %s\n\n", data)
						}
						switch wire {
						case "responses":
							emit("response.created", map[string]interface{}{"type": "response.created", "response": map[string]interface{}{"id": "resp_cache", "model": "cache-model", "status": "in_progress"}})
							emit("response.output_text.delta", map[string]interface{}{"type": "response.output_text.delta", "item_id": "msg_cache", "output_index": 0, "content_index": 0, "delta": "cache result"})
							emit("response.completed", map[string]interface{}{"type": "response.completed", "response": map[string]interface{}{"id": "resp_cache", "model": "cache-model", "status": "completed", "output": []interface{}{message}, "usage": nativeUsage}})
						case "openai":
							emit("", map[string]interface{}{"id": "chat_cache", "model": "cache-model", "choices": []interface{}{map[string]interface{}{"index": 0, "delta": map[string]interface{}{"role": "assistant", "content": "cache result"}, "finish_reason": nil}}})
							emit("", map[string]interface{}{"id": "chat_cache", "model": "cache-model", "choices": []interface{}{map[string]interface{}{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"}}, "usage": chatUsage})
							fmt.Fprint(w, "data: [DONE]\n\n")
						case "claude":
							emit("message_start", map[string]interface{}{"type": "message_start", "message": map[string]interface{}{"id": "msg_cache", "model": "cache-model", "type": "message", "role": "assistant", "content": []interface{}{}, "usage": claudeUsage}})
							emit("content_block_start", map[string]interface{}{"type": "content_block_start", "index": 0, "content_block": map[string]interface{}{"type": "text", "text": ""}})
							emit("content_block_delta", map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "text_delta", "text": "cache result"}})
							emit("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": 0})
							emit("message_delta", map[string]interface{}{"type": "message_delta", "delta": map[string]interface{}{"stop_reason": "end_turn"}, "usage": claudeUsage})
							emit("message_stop", map[string]interface{}{"type": "message_stop"})
						}
					}))
					defer upstream.Close()
					router := newResponsesTestRouter(t, config.UpstreamConfig{Name: "cached-usage", BaseURL: upstream.URL, APIKeys: []string{"test-key"}, ServiceType: wire, Status: "active"}, session.NewSessionManager(time.Hour, 100, 100000))
					response := performResponsesHandlerRequest(t, router, fmt.Sprintf(`{"model":"cache-model","input":"This prompt has been cached, so a zero or single uncached token count is valid.","stream":%t,"store":false}`, mode == "stream"))
					if response.Code != 200 {
						t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
					}
					var result struct {
						Usage struct {
							Input  uint64 `json:"input_tokens"`
							Cached struct {
								Tokens uint64 `json:"cached_tokens"`
							} `json:"input_tokens_details"`
							Output uint64 `json:"output_tokens"`
							Total  uint64 `json:"total_tokens"`
						} `json:"usage"`
					}
					payload := response.Body.Bytes()
					if mode == "stream" {
						completed := 0
						for _, line := range strings.Split(response.Body.String(), "\n") {
							if !strings.HasPrefix(line, "data: ") {
								continue
							}
							var event struct {
								Type     string          `json:"type"`
								Response json.RawMessage `json:"response"`
							}
							if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) == nil && event.Type == "response.completed" {
								completed++
								payload = event.Response
							}
						}
						if completed != 1 {
							t.Fatalf("completed=%d stream=%s", completed, response.Body.String())
						}
					}
					if err := json.Unmarshal(payload, &result); err != nil {
						t.Fatalf("unsigned client usage decode failed: %v; payload=%s", err, payload)
					}
					usage := result.Usage
					if usage.Input != uint64(sample.input) || usage.Cached.Tokens != uint64(sample.cached) || usage.Output != uint64(sample.output) || usage.Total != uint64(sample.total) || usage.Input < usage.Cached.Tokens || usage.Input-usage.Cached.Tokens != uint64(sample.input-sample.cached) || calls.Load() != 1 {
						t.Fatalf("usage changed or cache counted twice: got=%+v want=%+v calls=%d", usage, sample, calls.Load())
					}
				})
			}
		}
	}
}

func TestResponsesTinyCachedInputRequiresCompleteConvertedUsage(t *testing.T) {
	for _, sample := range []struct {
		name                 string
		input, cached, total int
		converted, patch     bool
	}{
		{"complete_zero", 0, 9984, 10138, true, false},
		{"complete_one", 1, 9984, 10139, true, false},
		{"missing_total", 0, 9984, 0, true, true},
		{"inconsistent_total", 0, 9984, 154, true, true},
		{"no_cache", 0, 0, 154, true, true},
		{"negative_input", -1, 9984, 10137, true, true},
		{"negative_total", 0, 9984, -10138, true, true},
		{"native_responses_not_internal", 0, 9984, 10138, false, true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			event := completedEventWithUsage(t, map[string]interface{}{"input_tokens": sample.input, "output_tokens": 154, "total_tokens": sample.total, "input_tokens_details": map[string]interface{}{"cached_tokens": sample.cached}})
			_, patch, collected := checkResponsesEventUsageWithLogTag(event, false, "", sample.converted)
			if patch != sample.patch {
				t.Fatalf("stream patch=%t, want %t", patch, sample.patch)
			}
			body := []byte(`{"model":"test","input":"An actual prompt that must be estimated when the upstream usage is missing or inconsistent."}`)
			response := &types.ResponsesResponse{Usage: types.ResponsesUsage{InputTokens: sample.input, OutputTokens: 154, TotalTokens: sample.total, InputTokensDetails: &types.InputTokensDetails{CachedTokens: sample.cached}}}
			patchResponsesUsageWithContext(nil, response, body, &config.EnvConfig{}, sample.converted)
			if changed := response.Usage.InputTokens != sample.input; changed != sample.patch {
				t.Fatalf("nonstream input changed=%t, want %t: %+v", changed, sample.patch, response.Usage)
			}
			patched := patchResponsesCompletedEventUsageWithLogTag(event, body, "answer", &collected, &config.EnvConfig{}, "", sample.converted)
			usage := usageField(t, patched, "")
			if changed := int(usage["input_tokens"].(float64)) != sample.input; changed != sample.patch {
				t.Fatalf("stream input changed=%t, want %t: %s", changed, sample.patch, patched)
			}
		})
	}
}
