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

// Like llama-server, the mock emits a usage-only final chunk only when requested.
// A local text estimate cannot substitute for the model's tokenizer or KV cache.
func TestResponsesChatRequestsAuthoritativeUsage(t *testing.T) {
	for _, mode := range []string{"stream", "json", "bridge"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				turn := int(calls.Add(1))
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if r.URL.Path != "/v1/chat/completions" || request["model"] != "qwen-local" {
					t.Errorf("invalid upstream request: %s %#v", r.URL.Path, request)
				}
				messages, _ := json.Marshal(request["messages"])
				if strings.Count(string(messages), "Preserve this developer instruction.") != 1 {
					t.Errorf("instruction duplicated or lost: %s", messages)
				}
				input, cached := 64256, 48000
				if turn == 2 {
					input, cached = 68000, 60000
				}
				usage := map[string]any{"prompt_tokens": input, "completion_tokens": 154, "total_tokens": input + 154, "prompt_tokens_details": map[string]any{"cached_tokens": cached}, "completion_tokens_details": map[string]any{"reasoning_tokens": 100}}
				if mode == "json" {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{"id": "chat_usage", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "reasoning_content": "analysis", "content": "answer"}, "finish_reason": "stop"}}, "usage": usage})
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"id\":\"chat_usage\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"analysis\"},\"finish_reason\":null}]}\n\n")
				fmt.Fprint(w, "data: {\"id\":\"chat_usage\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\n")
				fmt.Fprint(w, "data: {\"id\":\"chat_usage\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
				options, _ := request["stream_options"].(map[string]any)
				if options["include_usage"] == true {
					data, _ := json.Marshal(map[string]any{"id": "chat_usage", "choices": []any{}, "usage": usage})
					fmt.Fprintf(w, "data: %s\n\n", data)
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer upstream.Close()
			channel := config.UpstreamConfig{Name: "usage", BaseURL: upstream.URL, APIKeys: []string{"test-key"}, ServiceType: "openai", Status: "active"}
			router := newResponsesTestRouter(t, channel, session.NewSessionManager(time.Hour, 100, 100000))
			for turn := 1; turn <= 2; turn++ {
				options := ""
				if mode == "bridge" {
					options = `,"stream_options":{"include_usage":true}`
				}
				body := fmt.Sprintf(`{"model":"qwen-local","stream":%t,"store":false%s,"input":[{"type":"message","role":"developer","content":"Preserve this developer instruction."},{"type":"message","role":"user","content":"round %d"}]}`, mode == "stream", options, turn)
				response := performResponsesHandlerRequest(t, router, body)
				if response.Code != 200 {
					t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
				}
				var output map[string]any
				if mode == "stream" {
					terminals := 0
					for _, line := range strings.Split(response.Body.String(), "\n") {
						if !strings.HasPrefix(line, "data:") {
							continue
						}
						var event map[string]any
						if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event) == nil && event["type"] == "response.completed" {
							terminals++
							output, _ = event["response"].(map[string]any)
						}
					}
					if terminals != 1 {
						t.Fatalf("terminal count=%d: %s", terminals, response.Body.String())
					}
				} else if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
					t.Fatal(err)
				}
				usage, _ := output["usage"].(map[string]any)
				input, cached := float64(64256), float64(48000)
				if turn == 2 {
					input, cached = 68000, 60000
				}
				details, _ := usage["input_tokens_details"].(map[string]any)
				if usage["input_tokens"] != input || usage["output_tokens"] != float64(154) || usage["total_tokens"] != input+154 || details["cached_tokens"] != cached {
					t.Fatalf("lost authoritative usage, turn=%d: %#v", turn, usage)
				}
			}
			if calls.Load() != 2 {
				t.Fatalf("unexpected upstream calls=%d", calls.Load())
			}
		})
	}
}
