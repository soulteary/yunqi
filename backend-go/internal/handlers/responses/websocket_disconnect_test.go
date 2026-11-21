package responses

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestWebSocketBridgeAllowsOneQueuedTurn(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		round := calls.Add(1)
		if round == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, formatResponsesFoldSSE(map[string]interface{}{"type": "response.output_text.delta", "output_index": 0, "delta": "hello"}))
		_, _ = io.WriteString(w, formatResponsesFoldSSE(map[string]interface{}{"type": "response.completed", "response": map[string]interface{}{"id": fmt.Sprintf("resp_%d", round), "status": "completed", "output": []interface{}{}, "usage": map[string]interface{}{"input_tokens": 3, "output_tokens": 3, "total_tokens": 6}}}))
	}))
	defer upstream.Close()
	releaseUpstream := sync.OnceFunc(func() { close(release) })
	defer releaseUpstream()
	router := newResponsesWebSocketTestRouter(t, config.UpstreamConfig{BaseURL: upstream.URL, APIKeys: []string{"test-key"}, ServiceType: "responses", Status: "active"})
	proxy := httptest.NewServer(httpbridge.Handler(router))
	defer proxy.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/v1/responses", http.Header{"x-api-key": []string{"secret-key"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for i := 0; i < 2; i++ {
		if err := conn.WriteJSON(map[string]interface{}{"type": "response.create", "model": "test-model", "input": "test"}); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("upstream not started")
			}
		}
	}
	releaseUpstream()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for completed := 0; completed < 2; {
		event := readWebSocketJSON(t, conn)
		if event["type"] == "error" {
			t.Fatalf("bridge error: %v", event)
		}
		if event["type"] == "response.completed" {
			completed++
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("upstream calls=%d want=2", calls.Load())
	}
}

func TestWebSocketDisconnectCancelsUpstream(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%v", streaming), func(t *testing.T) {
			testWebSocketDisconnectCancelsUpstream(t, streaming)
		})
	}
}

func TestWebSocketQueuedInputIsBoundedAndCancelsUpstream(t *testing.T) {
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-release:
		}
	}))
	defer upstream.Close()
	releaseUpstream := sync.OnceFunc(func() { close(release) })
	defer releaseUpstream()
	router := newResponsesWebSocketTestRouter(t, config.UpstreamConfig{BaseURL: upstream.URL, APIKeys: []string{"test-key"}, ServiceType: "responses", Status: "active"})
	proxy := httptest.NewServer(httpbridge.Handler(router))
	defer proxy.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/v1/responses", http.Header{"x-api-key": []string{"secret-key"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	request := map[string]interface{}{"type": "response.create", "model": "test-model", "input": "test"}
	if err := conn.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream not started")
	}
	for range 2 {
		if err := conn.WriteJSON(request); err != nil {
			t.Fatal(err)
		}
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := conn.ReadMessage(); !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
		t.Fatalf("queue overflow error=%v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("queue overflow did not cancel upstream")
	}
}

func testWebSocketDisconnectCancelsUpstream(t *testing.T, streaming bool) {
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if streaming {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, formatResponsesFoldSSE(map[string]interface{}{"type": "response.created", "response": map[string]interface{}{"id": "audit", "status": "in_progress", "output": []interface{}{}}}))
			_, _ = io.WriteString(w, formatResponsesFoldSSE(map[string]interface{}{"type": "response.output_text.delta", "output_index": 0, "delta": "hello"}))
			w.(http.Flusher).Flush()
		}
		close(started)
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-release:
		}
	}))
	defer upstream.Close()
	releaseUpstream := sync.OnceFunc(func() { close(release) })
	defer releaseUpstream()
	router := newResponsesWebSocketTestRouter(t, config.UpstreamConfig{BaseURL: upstream.URL, APIKeys: []string{"test-key"}, ServiceType: "responses", Status: "active"})
	proxy := httptest.NewServer(httpbridge.Handler(router))
	defer proxy.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/v1/responses", http.Header{"x-api-key": []string{"secret-key"}})
	if err != nil {
		releaseUpstream()
		t.Fatal(err)
	}
	defer conn.Close()
	if err = conn.WriteJSON(map[string]interface{}{"type": "response.create", "model": "test-model", "input": "test"}); err != nil {
		conn.Close()
		releaseUpstream()
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		conn.Close()
		releaseUpstream()
		t.Fatal("upstream request not started")
	}
	if streaming {
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		for readWebSocketJSON(t, conn)["type"] != "response.output_text.delta" {
		}
	}
	conn.Close()
	stopped := false
	select {
	case <-canceled:
		stopped = true
	case <-time.After(time.Second):
	}
	releaseUpstream()
	if !stopped {
		t.Fatal("WebSocket client disconnected but pending upstream request context remained active")
	}
}
