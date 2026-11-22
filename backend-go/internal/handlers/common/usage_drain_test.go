package common

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/warmup"
)

func TestUsageDrainPolicyIsOptInAndCostBounded(t *testing.T) {
	for _, tc := range []struct {
		name, body            string
		timeout               int
		disabled, ws, enabled bool
	}{
		{"off", `{"max_output_tokens":16}`, 0, false, false, false},
		{"no-budget", `{}`, 100, false, false, false},
		{"large-budget", `{"max_tokens":1025}`, 100, false, false, false},
		{"fractional-budget", `{"max_tokens":1.5}`, 100, false, false, false},
		{"explicit", `{"max_output_tokens":16}`, 100, false, false, true},
		{"gemini", `{"generationConfig":{"maxOutputTokens":16}}`, 100, false, false, true},
		{"racing", `{"max_tokens":16}`, 100, true, false, false},
		{"websocket", `{"max_tokens":16}`, 100, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.ws {
				ctx = WithoutUsageDrain(ctx)
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.invalid", strings.NewReader(tc.body))
			_, state := prepareUsageDrainRequest(req, &config.EnvConfig{UsageDrainTimeoutMs: tc.timeout, ServiceContext: context.Background()}, true, tc.disabled)
			if (state != nil) != tc.enabled {
				t.Fatalf("enabled=%v", state != nil)
			}
			if state != nil {
				state.close()
			}
		})
	}
}

func TestUsageDrainDefaultImmediatelyCancelsHTTP(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {}\n\n")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader(`{"max_tokens":16}`))
	env := config.NewEnvConfig()
	req, state := prepareUsageDrainRequest(req, env, true, false)
	if state != nil {
		t.Fatal("default enabled drain")
	}
	resp, err := SendRequest(req, &config.UpstreamConfig{}, env, true, "Chat")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	<-started
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("default cancellation did not reach upstream")
	}
}

func TestExecutorUsageDrainKeepsFinalUsageAndNeverWritesAfterCancel(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "gzip"}[compressed], func(t *testing.T) { testExecutorUsageDrainKeepsFinalUsage(t, compressed) })
	}
}
func testExecutorUsageDrainKeepsFinalUsage(t *testing.T, compressed bool) {
	final := make(chan struct{})
	serverClosed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverClosed)
		w.Header().Set("Content-Type", "text/event-stream")
		var output io.Writer = w
		var zip *gzip.Writer
		if compressed {
			w.Header().Set("Content-Encoding", "gzip")
			zip = gzip.NewWriter(w)
			output = zip
			defer zip.Close()
		}
		_, _ = io.WriteString(output, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		if zip != nil {
			_ = zip.Flush()
		}
		w.(http.Flusher).Flush()
		select {
		case <-final:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(output, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	cm, sched, mm, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{Name: "drain", BaseURL: server.URL, APIKeys: []string{"test"}, Status: "active", ServiceType: "openai"})
	defer cleanup()
	up := &cm.GetConfig().Upstream[0]
	client, cancel := context.WithCancel(context.Background())
	defer cancel()
	raw := []byte(`{"model":"m","stream":true,"max_tokens":16}`)
	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(raw)).WithContext(client))
	env := config.NewEnvConfig()
	env.UsageDrainTimeoutMs = 500
	env.ServiceContext = context.Background()
	handled, _, _, _, usage, err := TryUpstreamWithAllKeys(c, env, cm, sched, scheduler.ChannelKindChat, "Chat", mm, up, []warmup.URLLatencyResult{{URL: server.URL}}, raw, nil, true,
		func(*config.UpstreamConfig, map[string]bool) (string, error) { return "test", nil }, func(c fiber.Ctx, _ *config.UpstreamConfig, _ string) (*http.Request, error) {
			req, e := http.NewRequestWithContext(httpbridge.Request(c).Context(), http.MethodPost, server.URL+"/v1/chat/completions", bytes.NewReader(raw))
			if compressed && e == nil {
				req.Header.Set("Accept-Encoding", "gzip")
			}
			return req, e
		}, func(string) {}, func(string) {}, func(string) {},
		func(c fiber.Ctx, resp *http.Response, _ *config.UpstreamConfig, _ string, _ []byte) (*types.Usage, error) {
			defer resp.Body.Close()
			buffer := make([]byte, 1024)
			if _, e := resp.Body.Read(buffer); e != nil {
				return nil, e
			}
			cancel()
			if _, e := httpbridge.Writer(c).Write([]byte("must not write")); !errors.Is(e, context.Canceled) {
				t.Fatalf("late write allowed: %v", e)
			}
			close(final)
			return nil, client.Err()
		}, "m", "", 0, nil)
	if !handled || !errors.Is(err, context.Canceled) || usage == nil || usage.InputTokens != 7 || usage.OutputTokens != 3 || w.Body.Len() != 0 {
		t.Fatalf("handled=%v usage=%#v err=%v body=%q", handled, usage, err, w.Body.String())
	}
	if usage.Provenance == nil || usage.Provenance.UsageSource != "provider" || !strings.Contains(string(usage.Provenance.RawUsage), "completion_tokens") {
		t.Fatalf("raw provenance missing: %#v", usage.Provenance)
	}
	<-serverClosed
}

func TestUsageDrainHardTimeoutAndServiceShutdown(t *testing.T) {
	for _, serviceStop := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "shutdown"}[serviceStop], func(t *testing.T) {
			stopped := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {}\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(stopped)
			}))
			defer server.Close()
			client, cancel := context.WithCancel(context.Background())
			defer cancel()
			service, shutdown := context.WithCancel(context.Background())
			defer shutdown()
			req, _ := http.NewRequestWithContext(client, http.MethodPost, server.URL, strings.NewReader(`{"max_tokens":16}`))
			env := config.NewEnvConfig()
			env.UsageDrainTimeoutMs = 50
			if serviceStop {
				env.UsageDrainTimeoutMs = 5000
			}
			env.ServiceContext = service
			req, state := prepareUsageDrainRequest(req, env, true, false)
			if state == nil {
				t.Fatal("policy not active")
			}
			defer state.close()
			resp, err := SendRequest(req, &config.UpstreamConfig{}, env, true, "Chat")
			if err != nil {
				t.Fatal(err)
			}
			body := newUsageDrainBody(&usageCapture{ReadCloser: resp.Body, stream: true}, state)
			buf := make([]byte, 100)
			_, _ = body.Read(buf)
			start := time.Now()
			cancel()
			if serviceStop {
				shutdown()
			}
			_ = body.Close()
			_ = body.finalUsage("chat")
			if time.Since(start) > time.Second {
				t.Fatal("drain exceeded bound")
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("drain did not cancel upstream")
			}
		})
	}
}

type boundedDrainSource struct {
	mu        sync.Mutex
	remaining int
	read      int
	closed    bool
}

func (s *boundedDrainSource) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.EOF
	}
	if s.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > s.remaining {
		n = s.remaining
	}
	for i := 0; i < n; i++ {
		p[i] = 'x'
	}
	s.remaining -= n
	s.read += n
	return n, nil
}
func (s *boundedDrainSource) Close() error { s.mu.Lock(); s.closed = true; s.mu.Unlock(); return nil }
func TestUsageDrainByteLimitAndCloseAfterRead(t *testing.T) {
	client, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(client, http.MethodPost, "https://example.invalid", strings.NewReader(`{"max_tokens":16}`))
	_, state := prepareUsageDrainRequest(req, &config.EnvConfig{UsageDrainTimeoutMs: 5000, ServiceContext: context.Background()}, true, false)
	defer state.close()
	cancel()
	source := &boundedDrainSource{remaining: 8 * usageDrainByteLimit}
	body := newUsageDrainBody(&usageCapture{ReadCloser: source, stream: true}, state)
	_ = body.Close()
	_ = body.finalUsage("chat")
	source.mu.Lock()
	defer source.mu.Unlock()
	if !source.closed || source.read != usageDrainByteLimit {
		t.Fatalf("read=%d closed=%v", source.read, source.closed)
	}
}

func TestUsageDrainPreservesNormalBytesAndOriginalDeadline(t *testing.T) {
	client, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(client, http.MethodPost, "https://example.invalid", strings.NewReader(`{"max_tokens":16}`))
	request, state := prepareUsageDrainRequest(req, &config.EnvConfig{UsageDrainTimeoutMs: 99999, ServiceContext: context.Background()}, true, false)
	if state == nil {
		t.Fatal("not enabled")
	}
	defer state.close()
	if state.timeout != 5*time.Second {
		t.Fatalf("timeout not clamped: %v", state.timeout)
	}
	original, _ := client.Deadline()
	actual, _ := request.Context().Deadline()
	if original != actual {
		t.Fatal("original deadline discarded")
	}
	raw := strings.Repeat("data: {}\n\n", 200000) + "data: [DONE]\n\n"
	body := newUsageDrainBody(&usageCapture{ReadCloser: io.NopCloser(strings.NewReader(raw)), stream: true}, state)
	actualBody, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil || string(actualBody) != raw {
		t.Fatalf("normal bytes changed err=%v len=%d", err, len(actualBody))
	}
}

func TestUsageDrainFinalUsageWithoutNewlinePreservesZero(t *testing.T) {
	client, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(client, http.MethodPost, "https://example.invalid", strings.NewReader(`{"max_tokens":16}`))
	_, state := prepareUsageDrainRequest(req, &config.EnvConfig{UsageDrainTimeoutMs: 100, ServiceContext: context.Background()}, true, false)
	defer state.close()
	cancel()
	raw := `data: {"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":0}}`
	body := newUsageDrainBody(&usageCapture{ReadCloser: io.NopCloser(strings.NewReader(raw)), stream: true}, state)
	usage := body.finalUsage("chat")
	if usage.InputTokens != 7 || usage.OutputTokens != 0 || usage.Provenance.UsageSource != "provider" || !strings.Contains(string(usage.Provenance.RawUsage), `"completion_tokens":0`) {
		t.Fatalf("final no-newline usage lost: %#v provenance=%#v", usage, usage.Provenance)
	}
}

func TestUsageDrainFinishedPumpWithEOFChunkAndSmallReads(t *testing.T) {
	for iteration := 0; iteration < 64; iteration++ {
		body := &usageDrainBody{lifetime: &usageDrainLifetime{client: context.Background()}, chunks: make(chan usageDrainChunk, 1), done: make(chan struct{}), pumpErr: io.EOF}
		expected := strings.Repeat("a", 4097)
		body.chunks <- usageDrainChunk{body: []byte(expected), err: io.EOF}
		close(body.done)
		var actual strings.Builder
		single := make([]byte, 1)
		for {
			n, err := body.Read(single)
			actual.Write(single[:n])
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if actual.String() != expected {
			t.Fatalf("finished pump truncated small Read: got=%d want=%d", actual.Len(), len(expected))
		}
	}
}
