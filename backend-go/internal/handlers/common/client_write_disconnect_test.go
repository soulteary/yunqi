package common

import (
	"context"
	"errors"
	"fmt"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/warmup"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestClientWriteDisconnectDoesNotPenalizeUpstream(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantFailed bool
	}{
		{"client_write_EPIPE", &net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", syscall.EPIPE)}, false},
		{"client_write_ECONNRESET", &net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", syscall.ECONNRESET)}, false},
		{"upstream_read_ECONNRESET", &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}, true},
		{"context_canceled_control", context.Canceled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {}\n\n"))
			}))
			defer server.Close()
			cm, sc, mm, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{Name: "audit", BaseURL: server.URL, APIKeys: []string{"DUMMY_AUDIT_KEY"}, Status: "active", ServiceType: "openai"})
			defer cleanup()
			cfg := cm.GetConfig()
			up := &cfg.Upstream[0]
			router := httpbridge.NewApp()
			router.Post("/v1/messages", func(c fiber.Ctx) error {
				TryUpstreamWithAllKeys(c, config.NewEnvConfig(), cm, sc, scheduler.ChannelKindMessages, "Messages", mm, up,
					[]warmup.URLLatencyResult{{URL: up.BaseURL}}, []byte("{\"model\":\"gpt-5.5\",\"messages\":[],\"stream\":true}"), nil, true,
					func(u *config.UpstreamConfig, failed map[string]bool) (string, error) {
						return cm.GetNextAPIKey(u, failed, "Messages")
					},
					func(c fiber.Ctx, u *config.UpstreamConfig, key string) (*http.Request, error) {
						return http.NewRequestWithContext(httpbridge.Request(c).Context(), http.MethodPost, u.BaseURL, strings.NewReader("{}"))
					},
					func(string) {}, nil, nil,
					func(c fiber.Ctx, resp *http.Response, u *config.UpstreamConfig, key string, body []byte) (*types.Usage, error) {
						defer resp.Body.Close()
						httpbridge.Writer(c).Header().Set("Content-Type", "text/event-stream")
						_, _ = httpbridge.Writer(c).Write([]byte("data: {}\n\n"))
						return nil, tc.err
					},
					"gpt-5.5", "", 0, sc.GetChannelLogStore(scheduler.ChannelKindMessages))
				return nil
			})
			w := httptest.NewRecorder()
			httpbridge.Handler(router).ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}")))
			got := cm.IsKeyFailed("DUMMY_AUDIT_KEY", "Messages")
			t.Logf("handleSuccess=%v keyFailed=%t want=%t", tc.err, got, tc.wantFailed)
			if got != tc.wantFailed {
				t.Errorf("key failed=%t, want %t", got, tc.wantFailed)
			}
			wantFailures := int64(0)
			if tc.wantFailed {
				wantFailures = 1
			}
			metric := mm.GetKeyMetrics(up.BaseURL, "DUMMY_AUDIT_KEY", "openai")
			if metric == nil {
				t.Fatal("missing request metrics")
			}
			if metric.RequestCount != 1 || metric.FailureCount != wantFailures || metric.ActiveRequests != 0 {
				t.Errorf("request metrics=%+v, want request=1 failures=%d active=0", metric, wantFailures)
			}
			if tracker := mm.ModelCircuit(); tracker != nil && int64(tracker.TrackedCount()) != wantFailures {
				t.Errorf("model circuit tracked=%d, want %d", tracker.TrackedCount(), wantFailures)
			}
		})
	}
}

func TestIsClientWriteDisconnect(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"broken pipe", syscall.EPIPE, true},
		{"wrapped broken pipe", fmt.Errorf("delivery failed: %w", syscall.EPIPE), true},
		{"write reset", &net.OpError{Op: "write", Err: os.NewSyscallError("write", syscall.ECONNRESET)}, true},
		{"wrapped write reset", fmt.Errorf("delivery: %w", &net.OpError{Op: "write", Err: syscall.ECONNRESET}), true},
		{"read reset", &net.OpError{Op: "read", Err: syscall.ECONNRESET}, false},
		{"dial reset", &net.OpError{Op: "dial", Err: syscall.ECONNRESET}, false},
		{"unqualified reset", syscall.ECONNRESET, false},
		{"generic message", errors.New("write: connection reset by peer"), false},
		{"deadline", context.DeadlineExceeded, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isClientWriteDisconnect(tt.err); got != tt.want {
				t.Fatalf("got=%t want=%t", got, tt.want)
			}
		})
	}
}
