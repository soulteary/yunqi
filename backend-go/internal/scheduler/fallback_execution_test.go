package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/quota"
	"github.com/soulteary/yunqi/internal/ratelimit"
)

func TestSelectChannelFederatedPressureFallbackPreservesExecution(t *testing.T) {
	for _, coldNative := range []bool{false, true} {
		t.Run(map[bool]string{false: "control", true: "native_pool_cold"}[coldNative], func(t *testing.T) {
			cfg := federationTestConfig()
			cfg.ChatUpstream[0].RateLimitRPM = 10
			s, cleanup := createTestScheduler(t, cfg)
			defer cleanup()
			s.SetModelSupportResolverProvider(newFederationResolver())
			limiter := s.GetRateLimitManager().GetOrCreate("Chat", 0, ratelimit.Config{RPM: 10, WindowSeconds: 60})
			for i := 0; i < 5; i++ {
				release, err := limiter.Acquire(context.Background(), time.Millisecond, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				release()
			}
			if coldNative {
				s.MarkChannelCooldown(ChannelKindMessages, 0, time.Minute)
			}
			result, err := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, Model: "audit", SmartFilter: func(ctx context.Context, channels []ChannelInfo) []ChannelInfo {
				filtered := pinKindFilter(ChannelKindChat)(ctx, channels)
				for i := range filtered {
					filtered[i].PinnedKeyIdentity = "audit-key-pin"
					filtered[i].PinnedEffort = "high"
				}
				return filtered
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("route=%+v reason=%s model=%q candidates=%d keyPin=%q effort=%q", result.Route, result.Reason, result.ExecutionModel, result.CandidateCount, result.ExecutionKeyIdentity, result.ExecutionEffort)
			if result.ExecutionModel != "kimi-k3" || result.CandidateCount != 1 || result.Reason != "rate_limit_pressure" || result.ExecutionKeyIdentity != "audit-key-pin" || result.ExecutionEffort != "high" {
				t.Fatal("request-kind cooldown corrupted sibling pressure fallback")
			}
		})
	}
}

func TestSelectChannelFederatedQuotaFallbackUsesPhysicalRoute(t *testing.T) {
	cfg := federationTestConfig()
	s, cleanup := createTestScheduler(t, cfg)
	defer cleanup()
	s.SetModelSupportResolverProvider(newFederationResolver())
	q := quota.NewManager()
	limit, remaining := 100.0, 0.0
	q.UpdateChannelProviderAPI("chat-k3", "acct-a", []quota.Value{{Dimension: quota.DimTokens, Limit: &limit, Remaining: &remaining}}, nil)
	if !q.IsChannelSaturated("chat-k3", time.Now().UnixMilli()) {
		t.Fatal("expected saturated quota control")
	}
	s.SetQuotaManager(q)
	s.MarkChannelCooldown(ChannelKindMessages, 0, time.Minute)
	result, err := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, Model: "audit", SmartFilter: pinKindFilter(ChannelKindChat)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != "quota_saturated_fallback" || result.Route.Kind != string(ChannelKindChat) || result.ExecutionModel != "kimi-k3" || result.CandidateCount != 1 {
		t.Fatalf("quota fallback lost execution: %+v", result)
	}
}
