package messages

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/providers"
	"github.com/soulteary/yunqi/internal/racing"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/thinkingcache"
)

func TestMessagesHandlerThinkingPresenceRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		states := []string{"empty", "nonempty", "absent"}
		if !stream {
			states = append(states, "empty_normalized_tool")
		}
		for _, state := range states {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, state), func(t *testing.T) {
				thinkingcache.ResetForTest()
				t.Cleanup(thinkingcache.ResetForTest)
				toolID := "toolu_flow"
				if state == "empty_normalized_tool" {
					toolID = "call_flow"
				}
				tool := map[string]interface{}{"type": "tool_use", "id": toolID, "name": "Bash", "input": map[string]interface{}{"command": "pwd"}}
				reasoning := ""
				if state == "nonempty" {
					reasoning = "actual upstream reasoning"
				}
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if calls.Add(1) == 1 {
						if stream {
							w.Header().Set("Content-Type", "text/event-stream")
							send := func(v interface{}) { data, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", data) }
							send(map[string]interface{}{"type": "message_start", "message": map[string]interface{}{"id": "msg_1", "type": "message", "role": "assistant", "model": "deepseek-v4-pro", "usage": map[string]int{"input_tokens": 10, "output_tokens": 1}}})
							index := 0
							if state != "absent" {
								send(map[string]interface{}{"type": "content_block_start", "index": index, "content_block": map[string]interface{}{"type": "thinking", "thinking": reasoning}})
								send(map[string]interface{}{"type": "content_block_stop", "index": index})
								index++
							}
							send(map[string]interface{}{"type": "content_block_start", "index": index, "content_block": tool})
							send(map[string]interface{}{"type": "content_block_stop", "index": index})
							send(map[string]interface{}{"type": "message_delta", "delta": map[string]interface{}{"stop_reason": "tool_use"}, "usage": map[string]int{"output_tokens": 2}})
							send(map[string]interface{}{"type": "message_stop"})
						} else {
							content := []interface{}{}
							if state != "absent" {
								content = append(content, map[string]interface{}{"type": "thinking", "thinking": reasoning})
							}
							content = append(content, tool)
							json.NewEncoder(w).Encode(map[string]interface{}{"id": "msg_1", "type": "message", "role": "assistant", "model": "deepseek-v4-pro", "content": content, "stop_reason": "tool_use", "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}})
						}
						return
					}
					data, _ := io.ReadAll(r.Body)
					var request struct {
						Messages []struct {
							Role    string
							Content []map[string]interface{}
						}
					}
					if err := json.Unmarshal(data, &request); err != nil {
						t.Errorf("decode request: %v", err)
					}
					var content []map[string]interface{}
					for _, msg := range request.Messages {
						if msg.Role == "assistant" {
							content = msg.Content
							break
						}
					}
					wantBlocks := 2
					if state == "absent" {
						wantBlocks = 1
					}
					valid := len(content) == wantBlocks
					if valid && state != "absent" {
						value, present := content[0]["thinking"].(string)
						valid = content[0]["type"] == "thinking" && present && value == reasoning && content[1]["type"] == "tool_use" && content[1]["name"] == "Bash"
					}
					if valid && state == "absent" {
						valid = content[0]["type"] == "tool_use"
					}
					if !valid {
						t.Errorf("history does not preserve %s upstream thinking: %s", state, data)
						w.WriteHeader(http.StatusBadRequest)
						io.WriteString(w, `{"error":{"message":"original thinking must be passed back","type":"invalid_request_error"}}`)
						return
					}
					io.WriteString(w, `{"id":"msg_2","type":"message","role":"assistant","model":"deepseek-v4-pro","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`)
				}))
				defer upstream.Close()
				channel := config.UpstreamConfig{BaseURL: upstream.URL, APIKeys: []string{"test-key"}, ServiceType: "claude", Status: "active", Priority: 1}
				channel.SetLearnedCompatTrait(config.TraitPassbackReasoningContent, true)
				channel.SetLearnedCompatTrait(config.TraitPassbackThinkingBlocks, true)
				router := newMessagesTestRouter(t, channel)
				perform := func(body string) *httptest.ResponseRecorder {
					req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("x-api-key", "secret-key")
					req.Header.Set("X-Claude-Code-Session-Id", "thinking-flow-session")
					w := httptest.NewRecorder()
					httpbridge.Handler(router).ServeHTTP(w, req)
					return w
				}
				first := perform(fmt.Sprintf(`{"model":"deepseek-v4-pro","stream":%t,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":[{"type":"text","text":"run pwd"}]}]}`, stream))
				if first.Code != http.StatusOK {
					t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
				}
				clientTool := map[string]interface{}{"type": "tool_use", "id": "toolu_flow", "name": "Bash", "input": map[string]interface{}{"command": "pwd"}}
				if state == "empty_normalized_tool" {
					var delivered struct{ Content []map[string]interface{} }
					if err := json.Unmarshal(first.Body.Bytes(), &delivered); err != nil {
						t.Fatal(err)
					}
					clientTool = delivered.Content[1]
				}
				// Client echoes the tool call without the upstream thinking block.
				secondBody := `{"model":"deepseek-v4-pro","thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":[{"type":"text","text":"run pwd"}]},{"role":"assistant","content":[{"type":"tool_use","id":"toolu_flow","name":"Bash","input":{"command":"pwd"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_flow","content":"/tmp"}]}]}`
				second := perform(strings.ReplaceAll(secondBody, "toolu_flow", clientTool["id"].(string)))
				if second.Code != http.StatusOK || !bytes.Contains(second.Body.Bytes(), []byte("done")) {
					t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
				}
				if calls.Load() != 2 {
					t.Fatalf("upstream calls=%d, want 2", calls.Load())
				}
			})
		}
	}
}

func TestNormalResponseRacingLoserDoesNotCacheThinking(t *testing.T) {
	for _, reasoning := range []string{"", "loser reasoning"} {
		t.Run(fmt.Sprintf("thinking=%q", reasoning), func(t *testing.T) {
			thinkingcache.ResetForTest()
			t.Cleanup(thinkingcache.ResetForTest)
			recorder := httptest.NewRecorder()
			c, _ := httpbridge.TestContext(recorder)
			requestBody := []byte(`{"model":"deepseek-v4-pro","thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hello"}]}`)
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(requestBody))
			req.Header.Set("X-Claude-Code-Session-Id", "racing-thinking")
			httpbridge.SetRequest(c, req)
			gate := racing.NewGate()
			gate.ClaimBy(1)
			httpbridge.SetValue(c, racing.ContextKeyGate, gate)
			content := []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": reasoning},
				map[string]interface{}{"type": "tool_use", "id": "toolu_loser", "name": "Bash", "input": map[string]interface{}{"command": "pwd"}},
			}
			body, _ := json.Marshal(map[string]interface{}{"id": "msg_loser", "type": "message", "role": "assistant", "content": content, "stop_reason": "tool_use", "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}})
			resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}
			_, err := handleNormalResponse(c, resp, &providers.ClaudeProvider{}, &config.EnvConfig{}, time.Now(), requestBody, &config.UpstreamConfig{ServiceType: "claude", BaseURL: "https://api.deepseek.com"}, "test-key")
			if !errors.Is(err, common.ErrRacingSuperseded) {
				t.Fatalf("error=%v", err)
			}
			if _, ok := thinkingcache.LookupClaudeThinkingForContent("racing-thinking", content); ok {
				t.Fatal("racing loser cached thinking")
			}
			if _, ok := thinkingcache.LookupLatestClaudeThinkingForSession("racing-thinking"); ok {
				t.Fatal("racing loser polluted session fallback")
			}
			if recorder.Body.Len() != 0 {
				t.Fatalf("racing loser wrote response: %s", recorder.Body.String())
			}
		})
	}
}
