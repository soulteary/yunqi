package common_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/racing"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/warmup"
)

func TestParentCancellationBetweenKeyAttemptsPreservesRacingWinner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"temporary upstream failure"}}`))
	}))
	defer server.Close()
	env := racingTestEnv(t, func(cfg *config.Config) {
		cfg.Upstream[0].BaseURL = server.URL
		cfg.Upstream[0].APIKeys = []string{"primary-first", "primary-second"}
	})
	defer env.cleanup()
	installRacingHub(t, racingCandidateList("ch_second"), racing.Behavior{MaxShadows: 1, StreamFloorMs: 30})
	claimed := make(chan struct{})
	retryReady := make(chan struct{})
	c := newRacingFiberContext()
	primary := racingPrimarySelection(t, env)
	keySelections := 0
	selected, result := common.RunRacingAttempt(c, func(c fiber.Ctx, s *scheduler.SelectionResult) common.MultiChannelAttemptResult {
		if s.Route.ChannelUID != "ch_first" {
			select {
			case <-retryReady:
			case <-time.After(2 * time.Second):
				t.Error("primary never reached retry boundary")
			}
			if !common.RacingClaimClientCommit(c) {
				return common.MultiChannelAttemptResult{Route: s.Route, LastError: common.ErrRacingSuperseded}
			}
			httpbridge.SetValue(c, "responseText", "shadow-response")
			close(claimed)
			return common.MultiChannelAttemptResult{Route: s.Route, Handled: true, Attempted: true, SuccessKey: "shadow-key", Usage: &types.Usage{OutputTokens: 42}, ResponseText: "shadow-response"}
		}
		cm := env.scheduler.GetConfigManager()
		handled, key, urlIndex, failover, usage, err := common.TryUpstreamWithAllKeys(c, config.NewEnvConfig(), cm, env.scheduler, scheduler.ChannelKindMessages, "Messages", env.scheduler.GetMetricsManagerForRoute(s.Route), s.Upstream, []warmup.URLLatencyResult{{URL: server.URL}}, []byte(`{"model":"test-model","stream":true}`), nil, true,
			func(u *config.UpstreamConfig, failed map[string]bool) (string, error) {
				keySelections++
				return cm.GetNextAPIKey(u, failed, "Messages")
			},
			func(c fiber.Ctx, u *config.UpstreamConfig, key string) (*http.Request, error) {
				return http.NewRequestWithContext(httpbridge.Request(c).Context(), http.MethodPost, u.BaseURL, strings.NewReader("{}"))
			},
			nil, func(string) {
				close(retryReady)
				select {
				case <-claimed:
				case <-time.After(2 * time.Second):
					t.Error("shadow failed to claim during primary retry")
				}
			}, nil,
			func(fiber.Ctx, *http.Response, *config.UpstreamConfig, string, []byte) (*types.Usage, error) {
				return nil, nil
			}, "test-model", "audit-user", s.ChannelIndex, env.scheduler.GetChannelLogStore(scheduler.ChannelKindMessages))
		return common.MultiChannelAttemptResult{Route: s.Route, Handled: handled, Attempted: true, SuccessKey: key, SuccessBaseURLIdx: urlIndex, FailoverError: failover, Usage: usage, LastError: err}
	}, racingInput(env, primary))
	if keySelections != 1 {
		t.Fatalf("losing primary attempted another key: %d", keySelections)
	}
	if selected.Route.ChannelUID != "ch_second" || result.SuccessKey != "shadow-key" || result.Usage == nil || result.Usage.OutputTokens != 42 || result.ResponseText != "shadow-response" {
		t.Fatalf("lost winning shadow settlement: route=%+v result=%+v", selected.Route, result)
	}
	if httpbridge.StringValue(c, "responseText") != "shadow-response" {
		t.Fatal("winning shadow values were not copied back")
	}
}
