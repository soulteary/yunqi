package metrics

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/types"
)

// CalculateUsageCostUSDWithPricingAndStatus evaluates a frozen pricing snapshot.
// Unknown dimensions return the known lower bound and false, never free usage.
func CalculateUsageCostUSDWithPricingAndStatus(pricing *config.ModelPricing, usage *types.Usage) (float64, bool) {
	if pricing == nil || usage == nil || !validUsageMeters(usage) || config.ValidateModelPricing(pricing) != nil {
		return 0, false
	}
	input, output, creation, read := extractUsageTokens(usage)
	if input < 0 || output < 0 || creation < 0 || read < 0 || usage.CacheCreation5mInputTokens < 0 || usage.CacheCreation1hInputTokens < 0 {
		return 0, false
	}
	currency, valid := usageCurrency(pricing)
	if !valid {
		return 0, false
	}
	effective := effectiveUsagePrices(pricing, input+creation+read)
	cost := 0.0
	known := true
	charge := func(units int64, price *float64) {
		if units == 0 {
			return
		}
		if price == nil {
			known = false
			return
		}
		cost += float64(units) * *price / 1e6
	}
	charge(input, effective.InputCacheMissPrice)
	charge(read, priceOrFallback(effective.InputCacheHitPrice, effective.InputCacheMissPrice))
	ttl5, ttl1 := int64(usage.CacheCreation5mInputTokens), int64(usage.CacheCreation1hInputTokens)
	if ttl5+ttl1 > creation {
		return cost * currency, false
	}
	if ttl5+ttl1 == 0 {
		switch strings.ToLower(usage.CacheTTL) {
		case "5m":
			ttl5 = creation
		case "1h":
			ttl1 = creation
		}
	}
	charge(ttl5, priceOrFallback(effective.CacheCreation5mPrice, effective.InputCacheMissPrice))
	charge(ttl1, priceOrFallback(effective.CacheCreation1hPrice, effective.InputCacheMissPrice))
	remaining := creation - ttl5 - ttl1
	if remaining > 0 && (effective.CacheCreation5mPrice != nil || effective.CacheCreation1hPrice != nil) {
		// A TTL-specific schedule cannot price unknown TTL as an ordinary input.
		known = false
	} else {
		charge(remaining, effective.InputCacheMissPrice)
	}
	reasoning, mode, meterKnown := reasoningMeter(usage)
	if !meterKnown && (mode != "" || effective.ReasoningOutputPrice != nil && output > 0) {
		known = false
	}
	if mode == "subset" {
		if reasoning > output {
			return cost * currency, false
		}
		charge(output-reasoning, effective.OutputPrice)
		charge(reasoning, priceOrFallback(effective.ReasoningOutputPrice, effective.OutputPrice))
	} else {
		charge(output, effective.OutputPrice)
		if mode == "additional" {
			charge(reasoning, priceOrFallback(effective.ReasoningOutputPrice, effective.OutputPrice))
		}
	}
	for _, image := range usage.ImageUnits {
		if image.Count < 0 {
			known = false
			continue
		}
		price, ok := imagePrice(pricing, image.Quality, image.Size)
		if !ok {
			known = false
			continue
		}
		cost += float64(image.Count) * *price
	}
	if cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
		return 0, false
	}
	return cost * currency, known && (hasAnyTokenPrice(pricing) || len(usage.ImageUnits) > 0)
}

// CalculateUsageReservationUSDWithPricing treats InputTokens and OutputTokens as
// upper bounds before cache/TTL/reasoning details are known. All reachable tiers
// participate, so a cache hit or a more expensive tier cannot exceed the reserve.
func CalculateUsageReservationUSDWithPricing(pricing *config.ModelPricing, upper *types.Usage) (float64, bool) {
	if pricing == nil || upper == nil || !validUsageMeters(upper) || config.ValidateModelPricing(pricing) != nil {
		return 0, false
	}
	currency, known := usageCurrency(pricing)
	if !known {
		return 0, false
	}
	input := int64(upper.InputTokens) + int64(upper.CacheReadInputTokens) + int64(upper.CacheCreationInputTokens)
	if upper.CacheReadInputTokens < 0 || upper.CacheCreationInputTokens < 0 {
		return 0, false
	}
	inputPrice, outputPrice, reasonPrice := 0.0, 0.0, 0.0
	inputKnown, outputKnown := true, true
	include := func(p config.ModelPricing) {
		if p.InputCacheMissPrice == nil {
			inputKnown = false
		}
		if p.OutputPrice == nil {
			outputKnown = false
		}
		for _, value := range []*float64{p.InputCacheMissPrice, p.InputCacheHitPrice, p.CacheCreation5mPrice, p.CacheCreation1hPrice} {
			if value != nil {
				inputPrice = math.Max(inputPrice, *value)
			}
		}
		if p.OutputPrice != nil {
			outputPrice = math.Max(outputPrice, *p.OutputPrice)
		}
		if p.ReasoningOutputPrice != nil {
			reasonPrice = math.Max(reasonPrice, *p.ReasoningOutputPrice)
		}
	}
	include(effectiveUsagePrices(pricing, input))
	for _, tier := range pricing.Tiers {
		for _, boundary := range []int64{0, int64(tier.InputTokensAbove) + 1, int64(tier.InputTokensUpTo) + 1} {
			if boundary <= input {
				include(effectiveUsagePrices(pricing, boundary))
			}
		}
	}
	if input > 0 && !inputKnown || upper.OutputTokens > 0 && !outputKnown {
		return 0, false
	}
	// Unknown vendor reasoning can be additional to candidates. Reserve both
	// upper bounds whenever a separate reasoning price is configured.
	cost := (float64(input)*inputPrice + float64(upper.OutputTokens)*(outputPrice+reasonPrice)) / 1e6
	for _, image := range upper.ImageUnits {
		if image.Count < 0 {
			return 0, false
		}
		price, ok := imagePrice(pricing, image.Quality, image.Size)
		if !ok {
			return 0, false
		}
		cost += float64(image.Count) * *price
	}
	if math.IsInf(cost, 0) || math.IsNaN(cost) {
		return 0, false
	}
	return cost * currency, true
}
func usageCurrency(p *config.ModelPricing) (float64, bool) {
	switch strings.ToUpper(strings.TrimSpace(p.Currency)) {
	case "", "USD":
		if strings.Contains(strings.ToLower(p.Unit), "cny") {
			return cnyToUSD, true
		}
		return 1, true
	case "CNY":
		return cnyToUSD, true
	default:
		return 0, false
	}
}
func effectiveUsagePrices(pricing *config.ModelPricing, input int64) config.ModelPricing {
	return effectiveUsagePricesForTier(pricing, selectPricingTier(pricing.Tiers, input))
}
func effectiveUsagePricesForTier(pricing *config.ModelPricing, tier *config.ModelPricingTier) config.ModelPricing {
	copy := *pricing
	if tier != nil {
		copy.InputCacheMissPrice = priceOrFallback(tier.InputCacheMissPrice, copy.InputCacheMissPrice)
		copy.InputCacheHitPrice = priceOrFallback(tier.InputCacheHitPrice, copy.InputCacheHitPrice)
		copy.OutputPrice = priceOrFallback(tier.OutputPrice, copy.OutputPrice)
		copy.CacheCreation5mPrice = priceOrFallback(tier.CacheCreation5mPrice, copy.CacheCreation5mPrice)
		copy.CacheCreation1hPrice = priceOrFallback(tier.CacheCreation1hPrice, copy.CacheCreation1hPrice)
		copy.ReasoningOutputPrice = priceOrFallback(tier.ReasoningOutputPrice, copy.ReasoningOutputPrice)
	}
	return copy
}
func imagePrice(p *config.ModelPricing, quality, size string) (*float64, bool) {
	score := -1
	var price *float64
	ambiguous := false
	for _, rule := range p.ImagePrices {
		if rule.Quality != "" && !strings.EqualFold(rule.Quality, quality) || rule.Size != "" && !strings.EqualFold(rule.Size, size) {
			continue
		}
		specific := 0
		if rule.Quality != "" {
			specific++
		}
		if rule.Size != "" {
			specific++
		}
		if specific > score {
			score = specific
			price = rule.Price
			ambiguous = false
		} else if specific == score && price != nil && rule.Price != nil && *price != *rule.Price {
			ambiguous = true
		}
	}
	return price, price != nil && !ambiguous
}
func reasoningMeter(u *types.Usage) (int64, string, bool) {
	if u.Provenance == nil {
		return 0, "", false
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(u.Provenance.RawUsage, &raw) != nil {
		return 0, "", false
	}
	read := func(data json.RawMessage) (int64, bool) {
		var n int64
		if json.Unmarshal(data, &n) != nil || n < 0 || n > 1<<50 {
			return 0, false
		}
		return n, true
	}
	switch strings.ToLower(u.Provenance.Protocol) {
	case "gemini":
		if value, ok := raw["thoughtsTokenCount"]; ok {
			n, valid := read(value)
			return n, "additional", valid
		}
		return 0, "additional", true
	case "openai", "chat", "responses":
		for _, key := range []string{"output_tokens_details", "completion_tokens_details"} {
			var details map[string]json.RawMessage
			if json.Unmarshal(raw[key], &details) == nil {
				if value, ok := details["reasoning_tokens"]; ok {
					n, valid := read(value)
					return n, "subset", valid
				}
			}
		}
		return 0, "subset", true
	case "messages", "claude":
		return 0, "", true
	default:
		return 0, "", false
	}
}

func validUsageMeters(u *types.Usage) bool {
	if len(u.ImageUnits) > 256 {
		return false
	}
	for _, n := range []int{u.InputTokens, u.OutputTokens, u.CacheCreationInputTokens, u.CacheReadInputTokens, u.CacheCreation5mInputTokens, u.CacheCreation1hInputTokens, u.PromptTokensTotal} {
		if n < 0 || int64(n) > 1<<50 {
			return false
		}
	}
	for _, image := range u.ImageUnits {
		if image.Count < 0 || image.Count > 1000000000 {
			return false
		}
	}
	return true
}
