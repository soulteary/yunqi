package messages

import (
	"bytes"
	"encoding/json"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Real net/http requests are cancelled after ServeHTTP returns, when Fiber has
// released its pooled context. No observer may retain that context afterwards.
func TestMessagesHandler_NonStreamRequestContextLifetime(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_lifetime","type":"message","role":"assistant","model":"claude-lifetime","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":2}}`)
	}))
	t.Cleanup(upstream.Close)
	router := newMessagesTestRouter(t, config.UpstreamConfig{
		Name: "context-lifetime", BaseURL: upstream.URL,
		APIKeys: []string{"test-key"}, ServiceType: "claude", AllowUnknownContext: true,
	})
	gateway := httptest.NewServer(httpbridge.Handler(router))
	t.Cleanup(gateway.Close)
	for i := 0; i < 8; i++ {
		body := bytes.NewBufferString(`{"model":"claude-lifetime","max_tokens":16,"messages":[{"role":"user","content":"test"}]}`)
		request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/messages", body)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("x-api-key", "secret-key")
		response, err := gateway.Client().Do(request)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		var result struct {
			Content []struct{ Text string } `json:"content"`
		}
		err = json.NewDecoder(response.Body).Decode(&result)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || len(result.Content) != 1 || result.Content[0].Text != "ok" {
			t.Fatalf("request %d: status=%d result=%+v err=%v", i, response.StatusCode, result, err)
		}
	}
}
