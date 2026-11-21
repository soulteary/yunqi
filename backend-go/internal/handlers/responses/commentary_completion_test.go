package responses

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/session"
)

func TestResponsesChatCommentaryContinuation(t *testing.T) {
	for _, ending := range []string{"answer", "tool", "commentary_only"} {
		t.Run(ending, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body, _ := io.ReadAll(r.Body)
				var req map[string]any
				if json.Unmarshal(body, &req) != nil || r.URL.Path != "/v1/chat/completions" || req["stream"] != true || req["model"] != "deepseek-v4-pro" {
					t.Errorf("unexpected upstream request %s %s", r.URL.Path, body)
					http.Error(w, "invalid request", 400)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				chunk := func(delta map[string]any, finish any) {
					data, _ := json.Marshal(map[string]any{"id": "chat-commentary", "model": "deepseek-v4-pro", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
					fmt.Fprintf(w, "data: %s\n\n", data)
					w.(http.Flusher).Flush()
				}
				for _, part := range []string{"<comm", "entary>Inspecting sources.", "</commentary>"} {
					chunk(map[string]any{"content": part}, nil)
				}
				finish := "stop"
				if ending == "answer" {
					chunk(map[string]any{"content": "\nFinal answer."}, nil)
				}
				if ending == "tool" {
					chunk(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call_inspect", "type": "function", "function": map[string]any{"name": "inspect", "arguments": "{\"path\":"}}}}, nil)
					chunk(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": "\"src\"}"}}}}, nil)
					finish = "tool_calls"
				}
				chunk(map[string]any{}, finish)
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer upstream.Close()
			router := newResponsesTestRouter(t, config.UpstreamConfig{Name: "commentary", BaseURL: upstream.URL, APIKeys: []string{"test-key"}, ServiceType: "openai", Status: "active"}, session.NewSessionManager(time.Hour, 100, 100000))
			response := performResponsesHandlerRequest(t, router, `{"model":"deepseek-v4-pro","input":"inspect","stream":true,"store":false,"tools":[{"type":"function","name":"inspect","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}]}`)
			if response.Code != 200 {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var completed map[string]any
			var deltas strings.Builder
			terminal := 0
			toolSeen := false
			for _, line := range strings.Split(response.Body.String(), "\n") {
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				var event map[string]any
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) != nil {
					continue
				}
				if terminal != 0 {
					t.Fatalf("event after completion: %s", line)
				}
				switch event["type"] {
				case "response.output_text.delta":
					deltas.WriteString(event["delta"].(string))
				case "response.output_item.done":
					item, _ := event["item"].(map[string]any)
					if item["type"] == "function_call" {
						toolSeen = item["name"] == "inspect" && item["call_id"] == "call_inspect" && item["arguments"] == `{"path":"src"}`
					}
				case "response.completed":
					terminal++
					completed, _ = event["response"].(map[string]any)
				}
			}
			want := "<commentary>Inspecting sources.</commentary>"
			if ending == "answer" {
				want += "\nFinal answer."
			}
			if deltas.String() != want || terminal != 1 || completed["status"] != "completed" || (ending == "tool" && !toolSeen) || calls.Load() != 1 {
				t.Fatalf("continuation lost or fabricated: text=%q terminal=%d tool=%v calls=%d stream=%s", deltas.String(), terminal, toolSeen, calls.Load(), response.Body.String())
			}
			var completedText strings.Builder
			output, _ := completed["output"].([]any)
			for _, raw := range output {
				item, _ := raw.(map[string]any)
				content, _ := item["content"].([]any)
				for _, rawPart := range content {
					part, _ := rawPart.(map[string]any)
					if part["type"] == "output_text" {
						text, _ := part["text"].(string)
						completedText.WriteString(text)
					}
				}
			}
			if completedText.String() != want {
				t.Fatalf("completed output changed text: %q", completedText.String())
			}
		})
	}
}
