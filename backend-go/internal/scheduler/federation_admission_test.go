package scheduler

import (
	"context"
	"github.com/soulteary/yunqi/internal/config"
	"testing"
	"time"
)

func TestProtocolFederationSurvivesNativeUnavailable(t *testing.T) {
	for _, state := range []string{"unsupported", "disabled", "context"} {
		t.Run(state, func(t *testing.T) {
			cfg := federationTestConfig()
			if state == "disabled" {
				cfg.Upstream[0].Status = "disabled"
			}
			if state == "context" {
				cfg.Upstream[0].ModelCapabilities = map[string]config.UpstreamModelCapability{"audit": {ContextWindowTokens: 1000}}
				cfg.ChatUpstream[0].ModelCapabilities = map[string]config.UpstreamModelCapability{"kimi-k3": {ContextWindowTokens: 1000000}}
			}
			s, cleanup := createTestScheduler(t, cfg)
			defer cleanup()
			resolver := newFederationResolver()
			s.SetModelSupportResolverProvider(func(ctx context.Context, kind ChannelKind, u *config.UpstreamConfig, model string) (bool, string, string, string) {
				if state == "unsupported" && kind == ChannelKindMessages && model != "" {
					return false, "", ModelSupportSourceAuthoritativeDeny, "unsupported"
				}
				return resolver(ctx, kind, u, model)
			})
			opts := SelectionOptions{Kind: ChannelKindMessages, Model: "audit", SmartFilter: pinKindFilter(ChannelKindChat)}
			if state == "context" {
				opts.ContextRequirement = &ContextRequirement{InputTokens: 500000, RequiredTokens: 508192}
			}
			// Prove the sibling is eligible independently of native admission.
			sibling := s.protocolFederationSiblings("acct-a", ChannelKindMessages)[0]
			var chat *config.UpstreamConfig
			for _, candidate := range s.protocolFederationSiblings("acct-a", ChannelKindMessages) {
				if candidate.Route.Kind == string(ChannelKindChat) {
					sibling = candidate
					chat = s.getUpstreamByRoute(candidate.Route)
					break
				}
			}
			if chat == nil || !s.channelAvailableForCandidateFilter(sibling, chat, ChannelKindChat, "") {
				t.Fatal("control: expected available chat sibling")
			}
			supported, actual, _, _ := resolver(context.Background(), ChannelKindChat, chat, opts.Model)
			if !supported {
				t.Fatal("control: sibling model unsupported")
			}
			if err := s.ValidateUpstreamContext(ChannelKindChat, actual, chat, opts.ContextRequirement); err != nil {
				t.Fatalf("control: sibling cannot fit: %v", err)
			}
			t.Logf("available physical count=%d", s.GetFederatedCandidateCount(ChannelKindMessages))
			result, err := s.SelectChannelWithOptions(context.Background(), opts)
			if err != nil {
				t.Fatalf("healthy chat sibling must remain reachable: %v", err)
			}
			if result.Upstream.ChannelUID != "chat-k3" {
				t.Fatalf("wrong route: %+v", result.Route)
			}
		})
	}
}

func TestProtocolFederationRecoversResponsesNativeAdmission(t *testing.T) {
	for _, state := range []string{"unsupported", "disabled", "context", "no_keys"} {
		t.Run(state, func(t *testing.T) {
			cfg := federationTestConfig()
			switch state {
			case "disabled":
				cfg.ResponsesUpstream[0].Status = "disabled"
			case "no_keys":
				cfg.ResponsesUpstream[0].APIKeys = nil
			case "context":
				cfg.ResponsesUpstream[0].ModelCapabilities = map[string]config.UpstreamModelCapability{"audit": {ContextWindowTokens: 1000}, "gpt-5.4": {ContextWindowTokens: 1000}}
				cfg.ChatUpstream[0].ModelCapabilities = map[string]config.UpstreamModelCapability{"kimi-k3": {ContextWindowTokens: 1000000}}
			}
			s, cleanup := createTestScheduler(t, cfg)
			defer cleanup()
			resolver := newFederationResolver()
			s.SetModelSupportResolverProvider(func(ctx context.Context, kind ChannelKind, u *config.UpstreamConfig, model string) (bool, string, string, string) {
				if state == "unsupported" && kind == ChannelKindResponses && u.ChannelUID == "responses-sib" && model != "" {
					return false, "", ModelSupportSourceAuthoritativeDeny, "unsupported"
				}
				return resolver(ctx, kind, u, model)
			})
			opts := SelectionOptions{Kind: ChannelKindResponses, Model: "audit", SmartFilter: pinKindFilter(ChannelKindChat)}
			if state == "context" {
				opts.ContextRequirement = &ContextRequirement{InputTokens: 500000, RequiredTokens: 508192}
			}
			result, err := s.SelectChannelWithOptions(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if result.Route.Kind != string(ChannelKindChat) || result.Upstream.ChannelUID != "chat-k3" || result.ExecutionModel != "kimi-k3" || result.CandidateCount != 1 {
				t.Fatalf("incorrect sibling execution: %+v", result)
			}
		})
	}
}

func TestProtocolFederationAdmissionPreservesExplicitConstraints(t *testing.T) {
	for _, mode := range []string{"pin", "prefix", "no_smart_filter", "sibling_cooldown", "sibling_unsupported", "failed_route"} {
		t.Run(mode, func(t *testing.T) {
			cfg := federationTestConfig()
			cfg.ResponsesUpstream[0].Status = "disabled"
			cfg.Upstream[0].ModelCapabilities = map[string]config.UpstreamModelCapability{"audit": {ContextWindowTokens: 1000}}
			cfg.ChatUpstream[0].ModelCapabilities = map[string]config.UpstreamModelCapability{"kimi-k3": {ContextWindowTokens: 1000000}}
			if mode == "prefix" {
				cfg.Upstream[0].RoutePrefix = "private"
			}
			s, cleanup := createTestScheduler(t, cfg)
			defer cleanup()
			resolver := newFederationResolver()
			s.SetModelSupportResolverProvider(func(ctx context.Context, kind ChannelKind, u *config.UpstreamConfig, model string) (bool, string, string, string) {
				if mode == "sibling_unsupported" && kind != ChannelKindMessages {
					return false, "", ModelSupportSourceAuthoritativeDeny, "unsupported"
				}
				return resolver(ctx, kind, u, model)
			})
			opts := SelectionOptions{Kind: ChannelKindMessages, Model: "audit", SmartFilter: pinKindFilter(ChannelKindChat), ContextRequirement: &ContextRequirement{InputTokens: 500000, RequiredTokens: 508192}}
			switch mode {
			case "pin":
				opts.ChannelName = "native"
			case "prefix":
				opts.RoutePrefix = "private"
			case "no_smart_filter":
				opts.SmartFilter = nil
			case "sibling_cooldown":
				s.MarkChannelCooldown(ChannelKindChat, 0, time.Minute)
			case "failed_route":
				opts.FailedRoutes = map[ChannelRouteKey]bool{channelRouteRef(ChannelKindChat, 0, &cfg.ChatUpstream[0]).Key(): true}
			}
			result, err := s.SelectChannelWithOptions(context.Background(), opts)
			if err == nil {
				t.Fatalf("constraint %s was bypassed: %+v", mode, result)
			}
		})
	}
}

func TestProtocolFederationAdmissionPrecedesOverflowRedirect(t *testing.T) {
	cfg := federationTestConfig()
	cfg.Upstream[0].ModelCapabilities = map[string]config.UpstreamModelCapability{"audit": {ContextWindowTokens: 1000}}
	cfg.ChatUpstream[0].ModelCapabilities = map[string]config.UpstreamModelCapability{"kimi-k3": {ContextWindowTokens: 1000000}}
	s, cleanup := createTestScheduler(t, cfg)
	defer cleanup()
	s.SetModelSupportResolverProvider(newFederationResolver())
	s.SetOverflowCandidateProvider(func(context.Context, ChannelKind, string, int) []ChannelInfo {
		t.Error("overflow redirect must not precede an eligible sibling")
		return nil
	})
	result, err := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, Model: "audit", SmartFilter: pinKindFilter(ChannelKindChat), ContextRequirement: &ContextRequirement{InputTokens: 500000, RequiredTokens: 508192}})
	if err != nil {
		t.Fatal(err)
	}
	if result.OverflowRedirect || result.Route.Kind != string(ChannelKindChat) || result.ExecutionModel != "kimi-k3" {
		t.Fatalf("sibling incorrectly treated as overflow: %+v", result)
	}
}

func TestProtocolFederationAdmissionStillScoresPromotedSiblings(t *testing.T) {
	cfg := federationTestConfig()
	cfg.Upstream[0].Status = "disabled"
	until := time.Now().Add(time.Hour)
	cfg.ChatUpstream[0].PromotionUntil = &until
	s, cleanup := createTestScheduler(t, cfg)
	defer cleanup()
	s.SetModelSupportResolverProvider(newFederationResolver())
	result, err := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, Model: "audit", SmartFilter: pinKindFilter(ChannelKindChat)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Route.Kind != string(ChannelKindChat) || result.Upstream.ChannelUID != "chat-k3" || result.Reason != "priority_order" || result.CandidateCount != 1 {
		t.Fatalf("sibling bypassed normal scoring: %+v", result)
	}
}
