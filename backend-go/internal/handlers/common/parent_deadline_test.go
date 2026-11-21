package common

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/warmup"
)

func TestTryUpstreamParentDeadlineDoesNotPenalizeKeys(t *testing.T) {
	for _, mode := range []string{"deadline", "cancel", "already_expired", "response_deadline", "upstream_deadline"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "response_deadline" {
					_, _ = w.Write([]byte(`{}`))
					return
				}
				select {
				case <-r.Context().Done():
				case <-time.After(time.Second):
				}
			}))
			defer server.Close()
			cm, sc, mm, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{Name: "deadline", BaseURL: server.URL, APIKeys: []string{"deadline-first", "deadline-second"}, Status: "active", ServiceType: "openai"})
			defer cleanup()
			up := cm.GetConfig().Upstream[0]
			router := httpbridge.NewApp()
			keySelections := 0
			var lastErr error
			handled := false
			router.Post("/v1/messages", func(c fiber.Ctx) error {
				parent := httpbridge.Request(c).Context()
				ctx, cancel := context.WithCancel(parent)
				defer cancel()
				switch mode {
				case "deadline", "response_deadline":
					deadlineCtx, deadlineCancel := context.WithTimeout(ctx, 200*time.Millisecond)
					defer deadlineCancel()
					ctx = deadlineCtx
				case "already_expired":
					deadlineCtx, deadlineCancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
					defer deadlineCancel()
					ctx = deadlineCtx
				case "cancel":
					timer := time.AfterFunc(200*time.Millisecond, cancel)
					defer timer.Stop()
				}
				httpbridge.SetRequest(c, httpbridge.Request(c).WithContext(ctx))
				handled, _, _, _, _, lastErr = TryUpstreamWithAllKeys(c, config.NewEnvConfig(), cm, sc, scheduler.ChannelKindMessages, "Messages", mm, &up, []warmup.URLLatencyResult{{URL: up.BaseURL}}, []byte(`{"model":"audit","messages":[],"stream":false}`), nil, false,
					func(u *config.UpstreamConfig, failed map[string]bool) (string, error) {
						keySelections++
						return cm.GetNextAPIKey(u, failed, "Messages")
					},
					func(c fiber.Ctx, u *config.UpstreamConfig, key string) (*http.Request, error) {
						requestCtx := httpbridge.Request(c).Context()
						if mode == "upstream_deadline" {
							child, childCancel := context.WithTimeout(requestCtx, 50*time.Millisecond)
							t.Cleanup(childCancel)
							requestCtx = child
						}
						return http.NewRequestWithContext(requestCtx, http.MethodPost, u.BaseURL, strings.NewReader("{}"))
					}, func(string) {}, nil, nil,
					func(c fiber.Ctx, resp *http.Response, u *config.UpstreamConfig, key string, body []byte) (*types.Usage, error) {
						defer resp.Body.Close()
						<-httpbridge.Request(c).Context().Done()
						return nil, httpbridge.Request(c).Context().Err()
					},
					"audit", "", 0, sc.GetChannelLogStore(scheduler.ChannelKindMessages))
				return nil
			})
			httpbridge.Handler(router).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}")))
			wantErr := context.DeadlineExceeded
			if mode == "cancel" {
				wantErr = context.Canceled
			}
			if !errors.Is(lastErr, wantErr) {
				t.Fatalf("expected %v, got %v", wantErr, lastErr)
			}
			wantFailed := mode == "upstream_deadline"
			wantSelections := 1
			if mode == "already_expired" {
				wantSelections = 0
			}
			if wantFailed {
				wantSelections = 2
			}
			if keySelections != wantSelections || handled == wantFailed {
				t.Errorf("key selections=%d handled=%t; want selections=%d handled=%t", keySelections, handled, wantSelections, !wantFailed)
			}
			for _, key := range up.APIKeys {
				if got := cm.IsKeyFailed(key, "Messages"); got != wantFailed {
					t.Errorf("key failure=%t want=%t", got, wantFailed)
				}
				if metric := mm.GetKeyMetrics(up.BaseURL, key, "openai"); metric != nil {
					wantFailures := int64(0)
					if wantFailed {
						wantFailures = 1
					}
					if metric.FailureCount != wantFailures || metric.ActiveRequests != 0 {
						t.Errorf("metrics failures=%d active=%d", metric.FailureCount, metric.ActiveRequests)
					}
				}
			}
			if !wantFailed && mm.ModelCircuit().TrackedCount() != 0 {
				t.Errorf("request cancellation polluted model circuit")
			}
		})
	}
}
