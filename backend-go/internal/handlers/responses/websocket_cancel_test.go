package responses

import (
	"context"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebSocketControlTargetsActiveResponse(t *testing.T) {
	var active webSocketActiveTurn
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	active.start(cancel)
	active.observe(`{"type":"response.created","response":{"id":"new"}}`)
	if active.interrupt("old") || ctx.Err() != nil {
		t.Fatal("stale response cancelled active turn")
	}
	if !active.interrupt("new") || ctx.Err() == nil {
		t.Fatal("active turn not cancelled")
	}
	active.finish()
	if active.interrupt("") {
		t.Fatal("cancelled inactive turn")
	}
}

func TestWebSocketCancelAllowsNextTurn(t *testing.T) {
	for _, control := range []string{"response.cancel", "response.interrupt"} {
		t.Run(control, func(t *testing.T) {
			var calls atomic.Int32
			canceled, release := make(chan struct{}), make(chan struct{})
			releaseOnce := sync.OnceFunc(func() { close(release) })
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				round := calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, fmt.Sprintf("event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_%d\"}}\n\n", round))
				_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\" world\"}\n\n")
				w.(http.Flusher).Flush()
				if round == 1 {
					select {
					case <-r.Context().Done():
						close(canceled)
					case <-release:
					}
					return
				}
				_, _ = io.WriteString(w, `event: response.completed
data: {"type":"response.completed","response":{"id":"resp_2","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}

`)
			}))
			defer upstream.Close()
			defer releaseOnce()
			router := newResponsesWebSocketTestRouter(t, config.UpstreamConfig{BaseURL: upstream.URL, APIKeys: []string{"test"}, ServiceType: "responses", Status: "active"})
			proxy := httptest.NewServer(httpbridge.Handler(router))
			defer proxy.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/v1/responses", http.Header{"x-api-key": []string{"secret-key"}})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			create := map[string]interface{}{"type": "response.create", "model": "m", "input": "hi"}
			if err := conn.WriteJSON(create); err != nil {
				t.Fatal(err)
			}
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			for {
				if event := readWebSocketJSON(t, conn); event["type"] == "response.output_text.delta" {
					break
				}
			}
			if err := conn.WriteJSON(map[string]interface{}{"type": control, "response_id": "resp_1"}); err != nil {
				t.Fatal(err)
			}
			event := readWebSocketJSONUntilType(t, conn, "response.cancelled")
			if event["type"] != "response.cancelled" {
				t.Fatalf("cancel acknowledgement: %v", event)
			}
			select {
			case <-canceled:
			case <-time.After(3 * time.Second):
				t.Fatal("upstream not cancelled")
			}
			if err := conn.WriteJSON(create); err != nil {
				t.Fatal(err)
			}
			for {
				event := readWebSocketJSON(t, conn)
				if event["type"] == "error" {
					t.Fatalf("next turn: %v", event)
				}
				if event["type"] == "response.completed" {
					break
				}
			}
			if calls.Load() != 2 {
				t.Fatalf("calls=%d", calls.Load())
			}
		})
	}
}
