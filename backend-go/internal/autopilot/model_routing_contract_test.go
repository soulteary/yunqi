package autopilot

import (
	"encoding/json"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
)

// Old per-channel aliases cannot bypass the documented protocol/profile policy.
func TestModelRoutingContractRetiredChannelAliases(t *testing.T) {
	for _, kind := range []string{"messages", "responses", "chat", "gemini", "images", "vectors"} {
		t.Run(kind, func(t *testing.T) {
			var cfg config.Config
			if err := json.Unmarshal([]byte(`{"upstream":[{"channelUid":"ch_test","baseUrl":"https://example.com","apiKeys":["test-key"],"serviceType":"claude","modelMapping":{"public-team-alias":"private-old"}}]}`), &cfg); err != nil {
				t.Fatal(err)
			}
			old := routingPolicyProfile(kind, "private-old", ModelFamilyUnknown)
			old.QualityTier = QualityTierLow
			best := routingPolicyProfile(kind, "private-best", ModelFamilyUnknown)
			best.QualityTier = QualityTierHigh
			resolver := newTestResolverWithConfig(t, []ModelProfile{old, best}, cfg)
			target, resolved, reason := resolver.ResolveModel("public-team-alias", "ch_test", kind, "metrics_test", CapabilityFloor{})
			if kind == "messages" || kind == "responses" {
				if !resolved || target.Model != "private-best" {
					t.Fatalf("adaptive routing must select by profile, got %s %v %s", target.Model, resolved, reason)
				}
			} else if resolved || target.Model != "public-team-alias" || reason != "exact_model_required" {
				t.Fatalf("exact-only routing must retain the original model, got %s %v %s", target.Model, resolved, reason)
			}
		})
	}
}

func TestModelRoutingContractPrivateModelAndMissingProfiles(t *testing.T) {
	for _, kind := range []string{"messages", "responses", "chat", "gemini", "images", "vectors"} {
		t.Run(kind, func(t *testing.T) {
			profile := routingPolicyProfile(kind, "private-team-model", ModelFamilyUnknown)
			resolver := newTestResolver(t, []ModelProfile{profile})
			target, resolved, reason := resolver.ResolveModel(profile.ModelID, "ch_test", kind, "metrics_test", CapabilityFloor{})
			if !resolved || target.Model != profile.ModelID || reason != "found_exact_model_in_profile" {
				t.Fatalf("private model must support exact matches, got %s %v %s", target.Model, resolved, reason)
			}
			target, resolved, reason = resolver.ResolveModel(profile.ModelID, "ch_test", kind, "metrics_test", CapabilityFloor{MinContextTokens: profile.ContextTokens + 1})
			if resolved || reason != "no_capable_model" {
				t.Fatalf("exact match must not bypass capability constraints, got %s %v %s", target.Model, resolved, reason)
			}
			empty := newTestResolver(t, nil)
			target, resolved, reason = empty.ResolveModel(profile.ModelID, "ch_test", kind, "metrics_test", CapabilityFloor{})
			if resolved || target.Model != profile.ModelID || reason != "no_model_profiles" {
				t.Fatalf("missing profiles must return the original model, got %s %v %s", target.Model, resolved, reason)
			}
		})
	}
}
