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
)

// A strict Chat endpoint rejects the flat Responses tool_choice, then checks
// that the function call returned to the client survives the next request.
func TestResponsesHandler_ChatNamedToolChoiceRoundTrip(t *testing.T) {
	for _, model := range []string{"gpt-5.5", "gpt-5.6", "arbitrary-chat-model"} {
		for _, compat := range []bool{false, true} {
			for _, toolName := range []string{"terminal", "plugin"} {
				t.Run(fmt.Sprintf("%s/compat=%v/%s", model, compat, toolName), func(t *testing.T) {
					var calls atomic.Int32
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						reject := func(reason string) {
							t.Errorf("invalid Chat request: %s", reason)
							http.Error(w, `{"error":{"message":"invalid Chat tool request"}}`, http.StatusBadRequest)
						}
						if r.URL.Path != "/v1/chat/completions" {
							reject("path=" + r.URL.Path)
							return
						}
						var req map[string]interface{}
						if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
							reject(err.Error())
							return
						}
						if req["model"] != model {
							reject(fmt.Sprint(req["model"]))
							return
						}
						tools, ok := req["tools"].([]interface{})
						if !ok || len(tools) != 2 {
							reject("missing tools")
							return
						}
						for _, raw := range tools {
							tool := raw.(map[string]interface{})
							fn, ok := tool["function"].(map[string]interface{})
							if !ok || fn["name"] == nil || fn["parameters"] == nil || tool["name"] != nil {
								reject("flat or malformed tool")
								return
							}
						}
						round := calls.Add(1)
						if round == 1 {
							choice, ok := req["tool_choice"].(map[string]interface{})
							if !ok {
								reject("missing choice")
								return
							}
							fn, ok := choice["function"].(map[string]interface{})
							if !ok || fn["name"] != toolName || choice["type"] != "function" || choice["name"] != nil {
								reject(fmt.Sprintf("tool_choice=%#v", choice))
								return
							}
							_, _ = fmt.Fprintf(w, `{"id":"chatcmpl_tool","model":%q,"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_contract","type":"function","function":{"name":%q,"arguments":"{\"command\":\"inspect\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`, model, toolName)
							return
						}
						if round != 2 {
							reject("unexpected retry")
							return
						}
						if req["tool_choice"] != "auto" {
							reject("second choice changed")
							return
						}
						messages, ok := req["messages"].([]interface{})
						if !ok {
							reject("missing messages")
							return
						}
						foundCall, foundResult := false, false
						for _, raw := range messages {
							message := raw.(map[string]interface{})
							if message["role"] == "tool" && message["tool_call_id"] == "call_contract" && message["content"] == "inspected" {
								foundResult = true
							}
							if toolCalls, ok := message["tool_calls"].([]interface{}); ok {
								for _, rawCall := range toolCalls {
									call := rawCall.(map[string]interface{})
									fn := call["function"].(map[string]interface{})
									if call["id"] == "call_contract" && fn["name"] == toolName && fn["arguments"] == `{"command":"inspect"}` {
										foundCall = true
									}
								}
							}
						}
						if !foundCall || !foundResult {
							reject(fmt.Sprintf("unpaired history: %#v", messages))
							return
						}
						_, _ = fmt.Fprintf(w, `{"id":"chatcmpl_done","model":%q,"choices":[{"message":{"role":"assistant","content":"completed"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":1,"total_tokens":8}}`, model)
					}))
					defer upstream.Close()
					router := newResponsesTestRouter(t, config.UpstreamConfig{Name: "strict-chat", BaseURL: upstream.URL, APIKeys: []string{"test-only"}, ServiceType: "openai", Status: "active", CodexToolCompat: &compat}, session.NewSessionManager(time.Hour, 100, 100000))
					tools := `[{"type":"function","name":"terminal","parameters":{"type":"object","properties":{"command":{"type":"string"}}}},{"type":"function","name":"plugin","parameters":{"type":"object","properties":{"command":{"type":"string"}}}}]`
					first := performResponsesHandlerRequest(t, router, fmt.Sprintf(`{"model":%q,"stream":false,"tools":%s,"tool_choice":{"type":"function","name":%q},"input":"inspect"}`, model, tools, toolName))
					if first.Code != http.StatusOK {
						t.Fatalf("first response status=%d body=%s", first.Code, first.Body.String())
					}
					var response map[string]interface{}
					if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					output, ok := response["output"].([]interface{})
					if !ok || len(output) != 1 {
						t.Fatalf("missing function call: %s", first.Body.String())
					}
					call := output[0].(map[string]interface{})
					if call["type"] != "function_call" || call["name"] != toolName || call["call_id"] != "call_contract" {
						t.Fatalf("incorrect call: %#v", call)
					}
					input, _ := json.Marshal([]interface{}{call, map[string]interface{}{"type": "function_call_output", "call_id": "call_contract", "output": "inspected"}})
					second := performResponsesHandlerRequest(t, router, fmt.Sprintf(`{"model":%q,"stream":false,"tools":%s,"tool_choice":"auto","input":%s}`, model, tools, input))
					if second.Code != http.StatusOK {
						t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
					}
					if calls.Load() != 2 {
						t.Fatalf("upstream calls=%d, want 2", calls.Load())
					}
					var final map[string]interface{}
					if err := json.Unmarshal(second.Body.Bytes(), &final); err != nil {
						t.Fatal(err)
					}
					if final["status"] != "completed" || !strings.Contains(second.Body.String(), `"text":"completed"`) {
						t.Fatalf("incomplete round trip: %s", second.Body.String())
					}
				})
			}
		}
	}
}
