package scheduler

import (
	"context"
	"github.com/soulteary/yunqi/internal/config"
	"testing"
	"time"
)

func TestSelectChannelOverflowPromotionKeepsPhysicalRoute(t *testing.T) {
	cfg := federationTestConfig()
	cfg.Upstream[0].ModelCapabilities = map[string]config.UpstreamModelCapability{"audit": {ContextWindowTokens: 1000}}
	until := time.Now().Add(time.Hour)
	cfg.ChatUpstream[0].PromotionUntil = &until
	s, cleanup := createTestScheduler(t, cfg)
	defer cleanup()
	s.SetOverflowCandidateProvider(func(context.Context, ChannelKind, string, int) []ChannelInfo {
		u := s.configManager.GetConfig().ChatUpstream[0]
		return []ChannelInfo{{Route: ChannelRouteRef{Kind: string(ChannelKindChat), Index: 0, ChannelUID: u.ChannelUID}, Index: 0, Name: u.Name, Status: "active", ActualModel: "kimi-k3", OverflowRedirect: true}}
	})
	result, err := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, Model: "audit", ContextRequirement: &ContextRequirement{InputTokens: 500000, RequiredTokens: 508192}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("selected route=%+v model=%q overflow=%t reason=%s", result.Route, result.ExecutionModel, result.OverflowRedirect, result.Reason)
	if result.Upstream.ChannelUID != "chat-k3" || result.ExecutionModel != "kimi-k3" || !result.OverflowRedirect {
		t.Fatal("promotion selected undersized native instead of overflow candidate")
	}
}

func TestSelectChannelOverflowWithoutPromotionControl(t *testing.T) {
	cfg := federationTestConfig()
	cfg.Upstream[0].ModelCapabilities = map[string]config.UpstreamModelCapability{"audit": {ContextWindowTokens: 1000}}
	s, cleanup := createTestScheduler(t, cfg)
	defer cleanup()
	s.SetOverflowCandidateProvider(func(context.Context, ChannelKind, string, int) []ChannelInfo {
		u := s.configManager.GetConfig().ChatUpstream[0]
		return []ChannelInfo{{Route: channelRouteRef(ChannelKindChat, 0, &u), Index: 0, Name: u.Name, Status: "active", ActualModel: "kimi-k3", OverflowRedirect: true}}
	})
	result, err := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, Model: "audit", ContextRequirement: &ContextRequirement{InputTokens: 500000, RequiredTokens: 508192}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Route.Kind != string(ChannelKindChat) || result.Upstream.ChannelUID != "chat-k3" || result.ExecutionModel != "kimi-k3" || !result.OverflowRedirect {
		t.Fatalf("control selected wrong overflow route: %+v", result)
	}
}

func TestSelectChannelOverflowPromotionUsesExecutionCooldown(t *testing.T) {
	for _, tc := range []struct {
		name        string
		coolingKind ChannelKind
		wantError   bool
	}{
		{"request_pool_cooldown", ChannelKindMessages, false},
		{"execution_pool_cooldown", ChannelKindChat, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := federationTestConfig()
			cfg.Upstream[0].ModelCapabilities = map[string]config.UpstreamModelCapability{"audit": {ContextWindowTokens: 1000}}
			until := time.Now().Add(time.Hour)
			cfg.ChatUpstream[0].PromotionUntil = &until
			s, cleanup := createTestScheduler(t, cfg)
			defer cleanup()
			s.SetOverflowCandidateProvider(func(context.Context, ChannelKind, string, int) []ChannelInfo {
				u := s.configManager.GetConfig().ChatUpstream[0]
				return []ChannelInfo{{Route: channelRouteRef(ChannelKindChat, 0, &u), Index: 0, Name: u.Name, Status: "active", ActualModel: "kimi-k3", OverflowRedirect: true}}
			})
			s.MarkChannelCooldown(tc.coolingKind, 0, time.Minute)
			result, err := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, Model: "audit", ContextRequirement: &ContextRequirement{InputTokens: 500000, RequiredTokens: 508192}})
			if tc.wantError {
				if err == nil {
					t.Fatalf("cooling execution route was selected: %+v", result)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Route.Kind != string(ChannelKindChat) || result.Upstream.ChannelUID != "chat-k3" || result.Reason != "promotion_priority" {
				t.Fatalf("request-pool cooldown affected sibling promotion: %+v", result)
			}
		})
	}
}
