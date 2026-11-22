// Package pricingtypes contains bounded, data-only pricing rules shared by
// runtime configuration and downloaded presets. Prices may explicitly be zero.
package pricingtypes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
)

// Token prices use per-million units in Currency. TTL prices replace ordinary
// cache creation input; reasoning replaces an output subset or adds Gemini
// thoughts according to captured provider metering. ImagePrices are additive
// per delivered image, independent of token rates. No expressions are evaluated.
type Pricing struct {
	VersionID            string           `json:"versionId,omitempty"`
	Unit                 string           `json:"unit,omitempty"`
	Currency             string           `json:"currency,omitempty"`
	InputCacheHitPrice   *float64         `json:"inputCacheHitPrice,omitempty"`
	InputCacheMissPrice  *float64         `json:"inputCacheMissPrice,omitempty"`
	OutputPrice          *float64         `json:"outputPrice,omitempty"`
	CacheCreation5mPrice *float64         `json:"cacheCreation5mPrice,omitempty"`
	CacheCreation1hPrice *float64         `json:"cacheCreation1hPrice,omitempty"`
	ReasoningOutputPrice *float64         `json:"reasoningOutputPrice,omitempty"`
	Tiers                []Tier           `json:"tiers,omitempty"`
	ImagePrices          []ImagePriceRule `json:"imagePrices,omitempty"`
}
type Tier struct {
	Label                string   `json:"label,omitempty"`
	InputTokensAbove     int      `json:"inputTokensAbove,omitempty"`
	InputTokensUpTo      int      `json:"inputTokensUpTo,omitempty"`
	InputCacheHitPrice   *float64 `json:"inputCacheHitPrice,omitempty"`
	InputCacheMissPrice  *float64 `json:"inputCacheMissPrice,omitempty"`
	OutputPrice          *float64 `json:"outputPrice,omitempty"`
	CacheCreation5mPrice *float64 `json:"cacheCreation5mPrice,omitempty"`
	CacheCreation1hPrice *float64 `json:"cacheCreation1hPrice,omitempty"`
	ReasoningOutputPrice *float64 `json:"reasoningOutputPrice,omitempty"`
}
type ImagePriceRule struct {
	Quality string   `json:"quality,omitempty"`
	Size    string   `json:"size,omitempty"`
	Price   *float64 `json:"price"`
}

func validPrices(values ...*float64) bool {
	for _, p := range values {
		if p != nil && (math.IsNaN(*p) || math.IsInf(*p, 0) || *p < 0) {
			return false
		}
	}
	return true
}
func Validate(p *Pricing) error {
	if p == nil {
		return nil
	}
	if len(p.Tiers) > 128 || len(p.ImagePrices) > 64 {
		return errors.New("pricing rule limit exceeded")
	}
	if !validPrices(p.InputCacheHitPrice, p.InputCacheMissPrice, p.OutputPrice, p.CacheCreation5mPrice, p.CacheCreation1hPrice, p.ReasoningOutputPrice) {
		return errors.New("prices must be finite nonnegative numbers")
	}
	for _, tier := range p.Tiers {
		if tier.InputTokensAbove < 0 || tier.InputTokensUpTo < 0 || int64(tier.InputTokensAbove) > 1<<50 || int64(tier.InputTokensUpTo) > 1<<50 || (tier.InputTokensUpTo > 0 && tier.InputTokensAbove >= tier.InputTokensUpTo) || !validPrices(tier.InputCacheHitPrice, tier.InputCacheMissPrice, tier.OutputPrice, tier.CacheCreation5mPrice, tier.CacheCreation1hPrice, tier.ReasoningOutputPrice) {
			return errors.New("invalid pricing tier")
		}
	}
	seen := map[string]bool{}
	for _, rule := range p.ImagePrices {
		if rule.Price == nil || !validPrices(rule.Price) || !validDimension(rule.Quality) || !validDimension(rule.Size) {
			return errors.New("invalid image pricing rule")
		}
		key := strings.ToLower(rule.Quality) + "\x00" + strings.ToLower(rule.Size)
		if seen[key] {
			return errors.New("duplicate image pricing rule")
		}
		seen[key] = true
	}
	return nil
}
func validDimension(value string) bool {
	if len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// VersionID always derives from content; callers cannot forge a snapshot's
// version by reusing a supplied versionId after changing a price.
func VersionID(p *Pricing) string {
	if p == nil {
		return ""
	}
	copy := *p
	copy.VersionID = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return "pricing_" + hex.EncodeToString(hash[:])
}
func Snapshot(p *Pricing) *Pricing {
	if p == nil {
		return nil
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	var copy Pricing
	if json.Unmarshal(data, &copy) != nil {
		return nil
	}
	copy.VersionID = VersionID(&copy)
	return &copy
}
