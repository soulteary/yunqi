package messages

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/thinkingcache"
)

// The same tool ID and assistant fingerprint may appear in independent sessions.
// Empty thinking must remain distinguishable from both absent and nonempty thinking.
func TestThinkingPresenceIsolatedAcrossInterleavedSessions(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			thinkingcache.ResetForTest()
			t.Cleanup(thinkingcache.ResetForTest)
			var mu sync.Mutex
			calls := make(map[string]int)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Messages []struct {
						Role    string                   `json:"role"`
						Content []map[string]interface{} `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) == 0 || len(body.Messages[0].Content) == 0 {
					t.Errorf("invalid upstream request: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				state, _ := body.Messages[0].Content[0]["text"].(string)
				mu.Lock()
				calls[state]++
				round := calls[state]
				mu.Unlock()
				if round > 1 {
					var assistant []map[string]interface{}
					for _, message := range body.Messages {
						if message.Role == "assistant" {
							assistant = message.Content
							break
						}
					}
					want := 2
					if state == "absent" {
						want = 1
					}
					valid := len(assistant) == want
					if valid && state != "absent" {
						thinking, exists := assistant[0]["thinking"].(string)
						wantText := ""
						if state == "nonempty" {
							wantText = "private reasoning for nonempty session"
						}
						valid = assistant[0]["type"] == "thinking" && exists && thinking == wantText
					}
					if valid {
						valid = assistant[len(assistant)-1]["type"] == "tool_use" && assistant[len(assistant)-1]["id"] == "toolu_shared"
					}
					if !valid {
						t.Errorf("session %s received another session's thinking or lost its own: %+v", state, assistant)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					_, _ = io.WriteString(w, `{"id":"msg_done","type":"message","role":"assistant","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`)
					return
				}
				content := []map[string]interface{}{}
				if state != "absent" {
					value := ""
					if state == "nonempty" {
						value = "private reasoning for nonempty session"
					}
					content = append(content, map[string]interface{}{"type": "thinking", "thinking": value})
				}
				content = append(content, map[string]interface{}{"type": "tool_use", "id": "toolu_shared", "name": "Bash", "input": map[string]interface{}{"command": "pwd"}})
				if !stream {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "msg_first", "type": "message", "role": "assistant", "content": content, "stop_reason": "tool_use", "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}})
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				send := func(event interface{}) { raw, _ := json.Marshal(event); _, _ = fmt.Fprintf(w, "data: %s\n\n", raw) }
				send(map[string]interface{}{"type": "message_start", "message": map[string]interface{}{"id": "msg_first", "type": "message", "role": "assistant", "model": "deepseek-v4-pro", "usage": map[string]int{"input_tokens": 10, "output_tokens": 1}}})
				for index, block := range content {
					send(map[string]interface{}{"type": "content_block_start", "index": index, "content_block": block})
					send(map[string]interface{}{"type": "content_block_stop", "index": index})
				}
				send(map[string]interface{}{"type": "message_delta", "delta": map[string]interface{}{"stop_reason": "tool_use"}, "usage": map[string]int{"output_tokens": 2}})
				send(map[string]interface{}{"type": "message_stop"})
			}))
			defer upstream.Close()
			channel := config.UpstreamConfig{BaseURL: upstream.URL, APIKeys: []string{"test-key"}, ServiceType: "claude", Status: "active", Priority: 1}
			channel.SetLearnedCompatTrait(config.TraitPassbackThinkingBlocks, true)
			channel.SetLearnedCompatTrait(config.TraitPassbackReasoningContent, true)
			app := newMessagesTestRouter(t, channel)
			perform := func(state string, replay bool) *httptest.ResponseRecorder {
				messages := []interface{}{map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "text", "text": state}}}}
				if replay {
					messages = append(messages,
						map[string]interface{}{"role": "assistant", "content": []interface{}{map[string]interface{}{"type": "tool_use", "id": "toolu_shared", "name": "Bash", "input": map[string]interface{}{"command": "pwd"}}}},
						map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "tool_result", "tool_use_id": "toolu_shared", "content": "/tmp"}}})
				}
				raw, _ := json.Marshal(map[string]interface{}{"model": "deepseek-v4-pro", "stream": stream && !replay, "thinking": map[string]interface{}{"type": "enabled", "budget_tokens": 1024}, "messages": messages})
				req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(raw)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("x-api-key", "secret-key")
				req.Header.Set("X-Claude-Code-Session-Id", "isolation-"+state)
				writer := httptest.NewRecorder()
				httpbridge.Handler(app).ServeHTTP(writer, req)
				return writer
			}
			for _, state := range []string{"empty", "nonempty", "absent"} {
				if result := perform(state, false); result.Code != http.StatusOK {
					t.Fatalf("first %s: status=%d body=%s", state, result.Code, result.Body.String())
				}
			}
			// Replay concurrently, after other sessions have populated the shared cache.
			var wg sync.WaitGroup
			for _, state := range []string{"absent", "empty", "nonempty"} {
				wg.Add(1)
				go func(state string) {
					defer wg.Done()
					if result := perform(state, true); result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "done") {
						t.Errorf("replay %s: status=%d body=%s", state, result.Code, result.Body.String())
					}
				}(state)
			}
			wg.Wait()
			for state, count := range calls {
				if count != 2 {
					t.Errorf("%s requests=%d, want exactly two without retries", state, count)
				}
			}
		})
	}
}
