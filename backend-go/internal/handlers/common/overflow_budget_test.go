package common_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/conversation"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/scheduler"
)

func TestHandleMultiChannelPromotedOverflowBudget(t *testing.T) {
	for _, mode := range []string{"control", "promoted", "manual_override"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.Config{
				Upstream: []config.UpstreamConfig{{Name: "native", ChannelUID: "native", AccountUID: "native-account", AutoManaged: true, Status: "active", BaseURL: "https://native.example.com", APIKeys: []string{"native-key"}, ModelCapabilities: map[string]config.UpstreamModelCapability{"audit": {ContextWindowTokens: 1000}}}},
				ChatUpstream: []config.UpstreamConfig{
					{Name: "first", ChannelUID: "overflow-first", AccountUID: "overflow-account", AutoManaged: true, Status: "active", BaseURL: "https://first.example.com", APIKeys: []string{"first-key"}},
					{Name: "second", ChannelUID: "overflow-second", AccountUID: "overflow-account", AutoManaged: true, Status: "active", BaseURL: "https://second.example.com", APIKeys: []string{"second-key"}},
				},
			}
			if mode == "promoted" {
				until := time.Now().Add(time.Hour)
				cfg.ChatUpstream[0].PromotionUntil = &until
			}
			env := newAffinityTestEnv(t, cfg)
			defer env.cleanup()
			if mode == "manual_override" {
				override := conversation.NewOverrideManager(time.Minute)
				defer override.Stop()
				env.scheduler.SetConversationComponents(nil, override)
				if err := override.SetOverride("audit-conversation", "messages", "audit-user", []conversation.ChannelEntry{{ChannelIndex: 0, ChannelName: "first"}}, time.Minute); err != nil {
					t.Fatal(err)
				}
			}
			env.scheduler.SetOverflowCandidateProvider(func(context.Context, scheduler.ChannelKind, string, int) []scheduler.ChannelInfo {
				var candidates []scheduler.ChannelInfo
				for i, u := range env.scheduler.GetConfigManager().GetConfig().ChatUpstream {
					candidates = append(candidates, scheduler.ChannelInfo{Route: scheduler.ChannelRouteRef{Kind: "chat", Index: i, ChannelUID: u.ChannelUID}, Index: i, Name: u.Name, Status: "active", Priority: i + 1, ActualModel: "big-model", OverflowRedirect: true})
				}
				return candidates
			})
			attempts := 0
			success := false
			allFailed := false
			common.HandleMultiChannelFailoverWithContextRequirement(newTestFiberContext(httptest.NewRecorder()), &config.EnvConfig{}, env.scheduler, scheduler.ChannelKindMessages, "Messages", "audit-user", "audit", &scheduler.ContextRequirement{InputTokens: 500000, RequiredTokens: 508192}, false, "",
				func(_ fiber.Ctx, s *scheduler.SelectionResult) common.MultiChannelAttemptResult {
					attempts++
					t.Logf("attempt=%d route=%+v reason=%s count=%d", attempts, s.Route, s.Reason, s.CandidateCount)
					if s.Route.ChannelUID == "overflow-first" {
						return common.MultiChannelAttemptResult{Route: s.Route, Attempted: true, LastError: errors.New("first overflow failed")}
					}
					success = true
					return common.MultiChannelAttemptResult{Route: s.Route, Attempted: true, Handled: true, SuccessKey: "second-key"}
				}, nil, func(fiber.Ctx, *common.FailoverError, error) { allFailed = true })
			if attempts != 2 || !success || allFailed {
				t.Fatalf("failover truncated: attempts=%d success=%t allFailed=%t", attempts, success, allFailed)
			}
		})
	}
}

func TestHandleMultiChannelNativePromotionKeepsFederatedBudget(t *testing.T) {
	until := time.Now().Add(time.Hour)
	cfg := config.Config{Upstream: []config.UpstreamConfig{{Name: "native", ChannelUID: "native", AccountUID: "shared", AutoManaged: true, BaseURL: "https://native.example.com", APIKeys: []string{"native-key"}, Status: "active", PromotionUntil: &until}}, ChatUpstream: []config.UpstreamConfig{{Name: "chat", ChannelUID: "chat", AccountUID: "shared", AutoManaged: true, BaseURL: "https://chat.example.com", APIKeys: []string{"chat-key"}, Status: "active"}}}
	env := newAffinityTestEnv(t, cfg)
	defer env.cleanup()
	env.scheduler.SetCandidateFilterProvider(func(context.Context, scheduler.ChannelKind, string) (scheduler.CandidateFilterFunc, scheduler.CandidateSelectionObserver) {
		return func(channels []scheduler.ChannelInfo, _ func(scheduler.ChannelInfo) *config.UpstreamConfig, _ func(scheduler.ChannelInfo, *config.UpstreamConfig) bool) ([]scheduler.ChannelInfo, error) {
			return channels, nil
		}, nil
	})
	env.scheduler.SetModelSupportResolverProvider(func(_ context.Context, _ scheduler.ChannelKind, _ *config.UpstreamConfig, model string) (bool, string, string, string) {
		return true, model, "test", ""
	})
	var routes []scheduler.ChannelRouteRef
	success := false
	common.HandleMultiChannelFailover(newTestFiberContext(httptest.NewRecorder()), &config.EnvConfig{}, env.scheduler, scheduler.ChannelKindMessages, "Messages", "audit-user", "audit", false, "",
		func(_ fiber.Ctx, s *scheduler.SelectionResult) common.MultiChannelAttemptResult {
			routes = append(routes, s.Route)
			if s.Route.ChannelUID == "native" {
				return common.MultiChannelAttemptResult{Route: s.Route, Attempted: true, LastError: errors.New("native failed")}
			}
			success = true
			return common.MultiChannelAttemptResult{Route: s.Route, Attempted: true, Handled: true, SuccessKey: "chat-key"}
		}, nil, nil)
	if !success || len(routes) != 2 || routes[1].Kind != "chat" {
		t.Fatalf("native promotion shrank federation budget: %+v", routes)
	}
}
