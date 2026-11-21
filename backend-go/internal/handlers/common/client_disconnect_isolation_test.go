package common

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/warmup"
)

// Exercise the real transport error, rather than constructing an EPIPE value.
// A client that resets its TCP connection must not poison the shared upstream
// key for another conversation, even after repeated disconnects.
func TestClientTCPDisconnectLeavesSharedKeyUsable(t *testing.T) {
	const apiKey = "test-shared-key"
	var upstreamRequests atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamRequests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n")
	}))
	defer upstreamServer.Close()
	cm, sc, mm, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{
		Name: "shared", BaseURL: upstreamServer.URL, APIKeys: []string{apiKey}, Status: "active", ServiceType: "openai",
	})
	defer cleanup()
	upstream := cm.GetConfig().Upstream[0]
	var urlFailures atomic.Int64
	type result struct {
		handled bool
		key     string
		err     error
	}
	finished := make(chan result, 1)
	app := httpbridge.NewApp()
	app.Post("/v1/messages", func(c fiber.Ctx) error {
		handled, key, _, _, _, lastErr := TryUpstreamWithAllKeys(
			c, config.NewEnvConfig(), cm, sc, scheduler.ChannelKindMessages, "Messages", mm, &upstream,
			[]warmup.URLLatencyResult{{URL: upstream.BaseURL}}, []byte(`{"model":"test-model","stream":true}`), nil, true,
			func(u *config.UpstreamConfig, failed map[string]bool) (string, error) {
				return cm.GetNextAPIKey(u, failed, "Messages")
			},
			func(c fiber.Ctx, u *config.UpstreamConfig, _ string) (*http.Request, error) {
				return http.NewRequestWithContext(httpbridge.Request(c).Context(), http.MethodPost, u.BaseURL, nil)
			},
			nil, func(string) { urlFailures.Add(1) }, nil,
			func(c fiber.Ctx, resp *http.Response, _ *config.UpstreamConfig, _ string, _ []byte) (*types.Usage, error) {
				defer resp.Body.Close()
				writer := httpbridge.Writer(c)
				writer.Header().Set("Content-Type", "text/event-stream")
				if _, err := io.WriteString(writer, "data: ready\n\n"); err != nil {
					return nil, err
				}
				writer.Flush()
				if httpbridge.Request(c).URL.Query().Get("disconnect") == "1" {
					select {
					case <-httpbridge.Request(c).Context().Done():
					case <-time.After(5 * time.Second):
						return nil, fmt.Errorf("test client did not disconnect")
					}
					// Exceed net/http's output buffer so Write reaches the reset socket.
					_, err := io.WriteString(writer, strings.Repeat("x", 64*1024))
					return nil, err
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_, err := io.WriteString(writer, "data: complete\n\n")
				return &types.Usage{InputTokens: 2, OutputTokens: 1}, err
			},
			"test-model", "", 0, sc.GetChannelLogStore(scheduler.ChannelKindMessages),
		)
		finished <- result{handled: handled, key: key, err: lastErr}
		return nil
	})
	proxy := httptest.NewServer(httpbridge.Handler(app))
	defer proxy.Close()
	awaitFinished := func() result {
		t.Helper()
		select {
		case r := <-finished:
			return r
		case <-time.After(5 * time.Second):
			t.Fatal("proxy did not finalize the request")
			return result{}
		}
	}
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	for round := 0; round < 3; round++ {
		conn, err := net.DialTimeout("tcp", strings.TrimPrefix(proxy.URL, "http://"), 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		tcp := conn.(*net.TCPConn)
		if err := tcp.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			tcp.Close()
			t.Fatal(err)
		}
		_, err = fmt.Fprintf(tcp, "POST /v1/messages?disconnect=1 HTTP/1.1\r\nHost: %s\r\nContent-Length: 0\r\n\r\n", strings.TrimPrefix(proxy.URL, "http://"))
		if err != nil {
			tcp.Close()
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(tcp), &http.Request{Method: http.MethodPost})
		if err != nil {
			tcp.Close()
			t.Fatal(err)
		}
		line, err := bufio.NewReader(resp.Body).ReadString('\n')
		if err != nil || line != "data: ready\n" {
			tcp.Close()
			resp.Body.Close()
			t.Fatalf("initial stream line = %q, err = %v", line, err)
		}
		if err := tcp.SetLinger(0); err != nil {
			tcp.Close()
			resp.Body.Close()
			t.Fatal(err)
		}
		_ = tcp.Close()
		_ = resp.Body.Close()
		canceled := awaitFinished()
		if !canceled.handled || canceled.key != "" || !isClientWriteDisconnect(canceled.err) && !errors.Is(canceled.err, context.Canceled) {
			t.Fatalf("round %d: reset produced %#v, want handled client write disconnect", round, canceled)
		}
		if cm.IsKeyFailed(apiKey, "Messages") || urlFailures.Load() != 0 {
			t.Fatalf("round %d: client reset penalized the upstream key or URL", round)
		}
		if got := mm.ModelCircuit().TrackedCount(); got != 0 {
			t.Fatalf("round %d: client reset created %d model circuit entries", round, got)
		}

		// A distinct conversation can immediately reuse the same single key.
		healthy, err := client.Post(proxy.URL+"/v1/messages", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(healthy.Body)
		_ = healthy.Body.Close()
		if err != nil || healthy.StatusCode != http.StatusOK || !strings.Contains(string(body), "data: complete") {
			t.Fatalf("round %d: healthy conversation status=%d body=%q err=%v", round, healthy.StatusCode, body, err)
		}
		success := awaitFinished()
		if !success.handled || success.key != apiKey || success.err != nil {
			t.Fatalf("round %d: healthy conversation did not reuse the shared key: %#v", round, success)
		}
	}
	metric := mm.GetKeyMetrics(upstream.BaseURL, apiKey, "openai")
	if metric == nil || metric.RequestCount != 6 || metric.FailureCount != 0 || metric.ActiveRequests != 0 {
		t.Fatalf("unexpected shared-key metrics after repeated resets and recovery: %+v", metric)
	}
	if got := upstreamRequests.Load(); got != 6 {
		t.Fatalf("upstream requests=%d, want exactly 6 (no replay after client reset)", got)
	}
}
