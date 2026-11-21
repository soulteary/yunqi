package responses

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestWebSocketBridgeReplaysKnownCursorWithoutHTTPPreviousResponseID(t *testing.T) {
	for _, store := range []bool{true, false} {
		t.Run(fmt.Sprintf("store=%t", store), func(t *testing.T) {
			requests := make(chan map[string]interface{}, 4)
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				requests <- body
				calls.Add(1)
				if body["previous_response_id"] != nil && body["input"] != "other connection" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"previous_response_id is only supported on Responses WebSocket v2"}}`))
					return
				}
				id := fmt.Sprintf("resp_%d", calls.Load())
				item := map[string]interface{}{"type": "message", "role": "assistant", "content": []interface{}{map[string]interface{}{"type": "output_text", "text": "answer " + id}}}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, event := range []map[string]interface{}{
					{"type": "response.created", "response": map[string]interface{}{"id": id, "status": "in_progress", "output": []interface{}{}}},
					{"type": "response.output_text.delta", "output_index": 0, "delta": "answer " + id},
					{"type": "response.output_item.done", "output_index": 0, "item": item},
					{"type": "response.completed", "response": map[string]interface{}{"id": id, "status": "completed", "output": []interface{}{item}, "usage": map[string]interface{}{"input_tokens": 3, "output_tokens": 3, "total_tokens": 6}}},
				} {
					_, _ = w.Write([]byte(formatResponsesFoldSSE(event)))
				}
			}))
			defer upstream.Close()
			router := newResponsesWebSocketTestRouter(t, config.UpstreamConfig{BaseURL: upstream.URL, APIKeys: []string{"test-key"}, ServiceType: "responses", Status: "active"})
			proxy := httptest.NewServer(httpbridge.Handler(router))
			defer proxy.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/v1/responses", http.Header{"x-api-key": []string{"secret-key"}})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			previous := ""
			for round := 1; round <= 3; round++ {
				request := map[string]interface{}{"type": "response.create", "model": "test-model", "input": fmt.Sprintf("question %d", round), "store": store}
				if previous != "" {
					request["previous_response_id"] = previous
				}
				if err := conn.WriteJSON(request); err != nil {
					t.Fatal(err)
				}
				for {
					event := readWebSocketJSON(t, conn)
					if event["type"] == "error" {
						t.Fatalf("round %d returned WebSocket error: %v", round, event)
					}
					if event["type"] == "response.completed" {
						previous = event["response"].(map[string]interface{})["id"].(string)
						break
					}
				}
				body := <-requests
				if body["previous_response_id"] != nil {
					t.Fatalf("round %d leaked resolved cursor to HTTP upstream", round)
				}
				if round > 1 {
					input, ok := body["input"].([]interface{})
					if !ok || len(input) != round*2-1 {
						t.Fatalf("round %d input=%v, want %d complete history items", round, body["input"], round*2-1)
					}
					encoded, _ := json.Marshal(input)
					for previousRound := 1; previousRound < round; previousRound++ {
						if !strings.Contains(string(encoded), fmt.Sprintf("question %d", previousRound)) || !strings.Contains(string(encoded), fmt.Sprintf("answer resp_%d", previousRound)) {
							t.Fatalf("round %d lost prior conversation: %s", round, encoded)
						}
					}
				}
			}
			if calls.Load() != 3 {
				t.Fatalf("upstream calls=%d, want one per turn", calls.Load())
			}
			other, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/v1/responses", http.Header{"x-api-key": []string{"secret-key"}})
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			if err := other.WriteJSON(map[string]interface{}{"type": "response.create", "model": "test-model", "previous_response_id": previous, "input": "other connection"}); err != nil {
				t.Fatal(err)
			}
			_ = readWebSocketJSONUntilType(t, other, "response.completed")
			foreign := <-requests
			if foreign["previous_response_id"] != previous || foreign["input"] != "other connection" {
				t.Fatalf("another connection reused private cursor history: %v", foreign)
			}
		})
	}
}
