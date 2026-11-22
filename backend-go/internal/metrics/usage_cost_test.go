package metrics

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/types"
	"math"
	"testing"
)

func TestTypedUsageCostReasoningDoesNotDoubleCountOpenAI(t *testing.T) {
	pricing := &config.ModelPricing{InputCacheMissPrice: floatPtr(1), OutputPrice: floatPtr(4), ReasoningOutputPrice: floatPtr(2)}
	for _, tc := range []struct {
		protocol, raw string
		want          float64
	}{{"responses", `{"output_tokens_details":{"reasoning_tokens":100000}}`, 3.8}, {"gemini", `{"thoughtsTokenCount":100000}`, 4.2}} {
		cost, known := CalculateUsageCostUSDWithPricingAndStatus(pricing, &types.Usage{OutputTokens: 1000000, Provenance: &types.UsageProvenance{Protocol: tc.protocol, RawUsage: json.RawMessage(tc.raw)}})
		if !known || math.Abs(cost-tc.want) > 1e-9 {
			t.Fatalf("%s cost=%v known=%v want=%v", tc.protocol, cost, known, tc.want)
		}
	}
	if cost, known := CalculateUsageCostUSDWithPricingAndStatus(pricing, &types.Usage{OutputTokens: 1000000}); known || cost != 2 {
		t.Fatalf("unknown reasoning basis must return a lower bound: cost=%v known=%v", cost, known)
	}
}
func TestTypedUsageCostCacheTTLAndInputTier(t *testing.T) {
	pricing := &config.ModelPricing{InputCacheMissPrice: floatPtr(1), CacheCreation5mPrice: floatPtr(2), CacheCreation1hPrice: floatPtr(4), Tiers: []config.ModelPricingTier{{InputTokensAbove: 100, InputCacheMissPrice: floatPtr(3), CacheCreation5mPrice: floatPtr(0)}}}
	cost, known := CalculateUsageCostUSDWithPricingAndStatus(pricing, &types.Usage{InputTokens: 1000000, CacheCreationInputTokens: 2000000, CacheCreation5mInputTokens: 1000000, CacheCreation1hInputTokens: 1000000})
	if !known || cost != 7 {
		t.Fatalf("TTL/tier cost=%v known=%v want7", cost, known)
	}
	ttlCost, ttlKnown := CalculateUsageCostUSDWithPricingAndStatus(pricing, &types.Usage{CacheCreationInputTokens: 1000000, CacheTTL: "1h"})
	if !ttlKnown || ttlCost != 4 {
		t.Fatalf("named TTL cost=%v known=%v", ttlCost, ttlKnown)
	}
	if _, known := CalculateUsageCostUSDWithPricingAndStatus(pricing, &types.Usage{CacheCreationInputTokens: 1}); known {
		t.Fatal("unknown TTL silently priced as ordinary input")
	}
}
func TestTypedImagePricingExactWildcardsAndUnknown(t *testing.T) {
	pricing := &config.ModelPricing{ImagePrices: []config.ImagePriceRule{{Price: floatPtr(.01)}, {Quality: "high", Size: "1024x1024", Price: floatPtr(.04)}, {Quality: "free", Price: floatPtr(0)}}}
	cost, known := CalculateUsageCostUSDWithPricingAndStatus(pricing, &types.Usage{ImageUnits: []types.ImageUsage{{Count: 2, Quality: "high", Size: "1024x1024"}, {Count: 3, Quality: "free"}}})
	if !known || math.Abs(cost-.08) > 1e-9 {
		t.Fatalf("image cost=%v complete=%v", cost, known)
	}
	if _, known := CalculateUsageCostUSDWithPricingAndStatus(pricing, &types.Usage{}); known {
		t.Fatal("unmetered image request treated as free")
	}
	missing := &config.ModelPricing{ImagePrices: []config.ImagePriceRule{{Quality: "high", Price: floatPtr(.04)}}}
	if _, known := CalculateUsageCostUSDWithPricingAndStatus(missing, &types.Usage{ImageUnits: []types.ImageUsage{{Count: 1, Quality: "standard"}}}); known {
		t.Fatal("unpriced image dimension treated as free")
	}
}
func TestTypedReservationBoundsEveryCacheTTLAndTier(t *testing.T) {
	p := &config.ModelPricing{InputCacheMissPrice: floatPtr(1), InputCacheHitPrice: floatPtr(8), OutputPrice: floatPtr(4), CacheCreation5mPrice: floatPtr(9), CacheCreation1hPrice: floatPtr(10), ReasoningOutputPrice: floatPtr(2), Tiers: []config.ModelPricingTier{{InputTokensAbove: 1, InputCacheMissPrice: floatPtr(3)}}}
	reserve, known := CalculateUsageReservationUSDWithPricing(p, &types.Usage{InputTokens: 1000000, OutputTokens: 1000000})
	if !known || reserve != 16 {
		t.Fatalf("reserve=%v known=%v want16", reserve, known)
	}
	for _, usage := range []*types.Usage{{InputTokens: 1000000}, {CacheCreationInputTokens: 1000000, CacheCreation5mInputTokens: 1000000}, {CacheCreationInputTokens: 1000000, CacheCreation1hInputTokens: 1000000}, {CacheReadInputTokens: 1000000}} {
		cost, complete := CalculateUsageCostUSDWithPricingAndStatus(p, usage)
		if !complete || cost > reserve {
			t.Fatalf("reservation below actual: cost=%v complete=%v", cost, complete)
		}
	}
	ttlReserve, ttlKnown := CalculateUsageReservationUSDWithPricing(p, &types.Usage{CacheCreation5mInputTokens: 1000000})
	if !ttlKnown || ttlReserve != 10 {
		t.Fatalf("TTL-only upper bound reserve=%v known=%v", ttlReserve, ttlKnown)
	}
	if _, known := CalculateUsageReservationUSDWithPricing(&config.ModelPricing{InputCacheHitPrice: floatPtr(0)}, &types.Usage{InputTokens: 1}); known {
		t.Fatal("unknown cache miss price accepted as free")
	}
}

func TestTypedReasoningMeterMissingIsNotExplicitZero(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "gemini"} {
		explicit := `{"output_tokens_details":{"reasoning_tokens":0}}`
		if protocol == "gemini" {
			explicit = `{"thoughtsTokenCount":0}`
		}
		for _, raw := range []string{`{}`, `{"thoughtsTokenCount":"0","output_tokens_details":{"reasoning_tokens":"0"}}`, `not-json`, explicit} {
			pricing := &config.ModelPricing{OutputPrice: floatPtr(4), ReasoningOutputPrice: floatPtr(2)}
			usage := &types.Usage{OutputTokens: 1000000, Provenance: &types.UsageProvenance{Protocol: protocol, RawUsage: json.RawMessage(raw)}}
			cost, known := CalculateUsageCostUSDWithPricingAndStatus(pricing, usage)
			wantKnown := raw == explicit
			want := 4.0
			if !wantKnown && protocol != "gemini" {
				want = 2
			}
			if known != wantKnown || cost != want {
				t.Fatalf("%s raw=%s cost=%v known=%v want=%v/%v", protocol, raw, cost, known, want, wantKnown)
			}
			pricing.ReasoningOutputPrice = nil
			cost, known = CalculateUsageCostUSDWithPricingAndStatus(pricing, usage)
			if !known || cost != 4 {
				t.Fatalf("ordinary pricing compatibility changed: %s raw=%s cost=%v known=%v", protocol, raw, cost, known)
			}
		}
	}
}
