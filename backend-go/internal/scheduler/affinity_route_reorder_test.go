package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
)

func TestSelectChannelAffinityRefreshesStaleIndex(t *testing.T) {
	for _, tc := range []struct {
		name                string
		cooldownIndex       int
		wantUID, wantReason string
		wantIndex           int
	}{
		{"reordered", -1, "a", "trace_affinity", 0},
		{"old_slot_cooling", 1, "a", "trace_affinity", 0},
		{"current_slot_cooling", 0, "b", "priority_order", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{Upstream: []config.UpstreamConfig{
				{Name: "removed", ChannelUID: "removed", BaseURL: "https://removed.example.com", APIKeys: []string{"removed-key"}, Status: "active", Priority: 1},
				{Name: "A", ChannelUID: "a", BaseURL: "https://a.example.com", APIKeys: []string{"a-key"}, Status: "active", Priority: 1},
				{Name: "B", ChannelUID: "b", BaseURL: "https://b.example.com", APIKeys: []string{"b-key"}, Status: "active", Priority: 1},
			}}
			s, cleanup := createTestScheduler(t, cfg)
			defer cleanup()
			s.SetTraceAffinityRouteForRequirement("audit-user", ChannelRouteRef{Kind: string(ChannelKindMessages), Index: 1, ChannelUID: "a"}, ChannelKindMessages, nil)
			if _, err := s.configManager.RemoveUpstream(0); err != nil {
				t.Fatal(err)
			}
			if tc.cooldownIndex >= 0 {
				s.MarkChannelCooldown(ChannelKindMessages, tc.cooldownIndex, time.Minute)
			}
			result, err := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, UserID: "audit-user"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Upstream.ChannelUID != tc.wantUID || result.Route.ChannelUID != tc.wantUID || result.Route.Index != tc.wantIndex || result.ChannelIndex != tc.wantIndex || result.Reason != tc.wantReason {
				t.Fatalf("got UID=%s route=%+v index=%d reason=%s; want UID=%s index=%d reason=%s", result.Upstream.ChannelUID, result.Route, result.ChannelIndex, result.Reason, tc.wantUID, tc.wantIndex, tc.wantReason)
			}
		})
	}
}

func TestSelectChannelFederatedAffinityRefreshesExecutionMetadata(t *testing.T) {
	cfg := federationTestConfig()
	// Deleting the preceding channel shifts the sibling from index 1 to 0.
	cfg.ChatUpstream[0], cfg.ChatUpstream[1] = cfg.ChatUpstream[1], cfg.ChatUpstream[0]
	s, cleanup := createTestScheduler(t, cfg)
	defer cleanup()
	s.SetModelSupportResolverProvider(newFederationResolver())
	s.SetTraceAffinityRouteForRequirement("audit-user", ChannelRouteRef{Kind: string(ChannelKindChat), Index: 1, ChannelUID: "chat-k3"}, ChannelKindMessages, nil)
	if _, err := s.configManager.RemoveChatUpstream(0); err != nil {
		t.Fatal(err)
	}
	result, err := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, UserID: "audit-user", Model: "audit", SmartFilter: pinKindFilter(ChannelKindChat)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Route.Kind != string(ChannelKindChat) || result.Route.Index != 0 || result.ChannelIndex != 0 || result.ExecutionModel != "kimi-k3" || result.Reason != "trace_affinity" {
		t.Fatalf("stale affinity corrupted execution metadata: %+v", result)
	}
}
