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

func TestClaudeToolSearchRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				round := calls.Add(1)
				tools, _ := body["tools"].([]interface{})
				foundSearch, foundRead, foundOrdinary := false, false, false
				for _, raw := range tools {
					tool := raw.(map[string]interface{})
					if tool["type"] == "tool_search" {
						t.Error("native tool_search leaked to Claude upstream")
					}
					if tool["name"] == "tool_search" {
						foundSearch = tool["input_schema"] != nil
					}
					if tool["name"] == "ordinary_tool" {
						foundOrdinary = true
					}
					if tool["name"] == "read_file" {
						foundRead = true
					}
				}
				if !foundSearch || !foundOrdinary || (round == 2 && !foundRead) {
					w.WriteHeader(400)
					_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"search tool or discovered function missing"}}`)
					return
				}
				name, id, args := "tool_search", "call_search", `{"query":"read a file"}`
				if round == 1 {
					choice, _ := body["tool_choice"].(map[string]interface{})
					if choice["type"] != "tool" || choice["name"] != "tool_search" {
						t.Errorf("Claude tool_choice=%v", body["tool_choice"])
					}
				} else {
					encoded, _ := json.Marshal(body["messages"])
					if !strings.Contains(string(encoded), `"type":"tool_use"`) || !strings.Contains(string(encoded), `"tool_use_id":"call_search"`) {
						t.Errorf("tool search history lost: %s", encoded)
					}
					name, id, args = "read_file", "call_read", `{"path":"notes.txt"}`
				}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					events := []map[string]interface{}{
						{"type": "message_start", "message": map[string]interface{}{"id": "msg_search", "type": "message", "role": "assistant", "usage": map[string]int{"input_tokens": 10, "output_tokens": 0}}},
						{"type": "content_block_start", "index": 0, "content_block": map[string]interface{}{"type": "tool_use", "id": id, "name": name, "input": map[string]interface{}{}}},
						{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "input_json_delta", "partial_json": args}},
						{"type": "content_block_stop", "index": 0},
						{"type": "message_delta", "delta": map[string]interface{}{"stop_reason": "tool_use"}, "usage": map[string]int{"output_tokens": 5}},
						{"type": "message_stop"},
					}
					for _, event := range events {
						data, _ := json.Marshal(event)
						_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
					}
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprintf(w, `{"id":"msg_search","type":"message","role":"assistant","model":"kimi-test","content":[{"type":"tool_use","id":%q,"name":%q,"input":%s}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`, id, name, args)
				}
			}))
			defer upstream.Close()
			enabled := true
			router := newResponsesTestRouter(t, config.UpstreamConfig{CodexToolCompat: &enabled, BaseURL: upstream.URL, APIKeys: []string{"test-key"}, ServiceType: "claude"}, session.NewSessionManager(time.Hour, 100, 100000))
			request := map[string]interface{}{"model": "kimi-test", "stream": stream, "store": false, "input": "find a file tool", "tools": []interface{}{map[string]interface{}{"type": "tool_search", "execution": "client", "parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]string{"type": "string"}}}}, map[string]interface{}{"type": "function", "name": "ordinary_tool", "parameters": map[string]interface{}{"type": "object"}}}, "tool_choice": map[string]interface{}{"type": "tool_search"}}
			data, _ := json.Marshal(request)
			first := performResponsesHandlerRequest(t, router, string(data))
			if first.Code != 200 || !strings.Contains(first.Body.String(), `"type":"tool_search_call"`) || strings.Contains(first.Body.String(), `"type":"custom_tool_call"`) {
				t.Fatalf("search response status=%d body=%s", first.Code, first.Body.String())
			}
			request["input"] = []interface{}{map[string]interface{}{"type": "tool_search_call", "call_id": "call_search", "execution": "client", "arguments": map[string]string{"query": "read a file"}}, map[string]interface{}{"type": "tool_search_output", "call_id": "call_search", "execution": "client", "tools": []interface{}{map[string]interface{}{"type": "function", "name": "read_file", "parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]string{"type": "string"}}}}}}}
			delete(request, "tool_choice")
			data, _ = json.Marshal(request)
			second := performResponsesHandlerRequest(t, router, string(data))
			if second.Code != 200 || !strings.Contains(second.Body.String(), `"name":"read_file"`) || !strings.Contains(second.Body.String(), `"type":"function_call"`) {
				t.Fatalf("discovered function response status=%d body=%s", second.Code, second.Body.String())
			}
			if calls.Load() != 2 {
				t.Fatalf("upstream called %d times, want two tool turns", calls.Load())
			}
		})
	}
}
