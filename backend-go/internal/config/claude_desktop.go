package config

import (
	"fmt"
	"maps"
	"strings"
	"unicode"
)

// NormalizeClaudeDesktopTier accepts the client's tier names, not model identities.
func NormalizeClaudeDesktopTier(value string) string {
	switch value = strings.ToLower(strings.TrimSpace(value)); value {
	case "sonnet", "opus", "haiku", "fable", "mythos":
		return value
	default:
		return ""
	}
}

// An explicit empty value suppresses inherited metadata for that model. A nil map
// is an omitted update; an empty map removes all dedicated overrides.
func NormalizeClaudeDesktopModelTiers(input map[string]string) (map[string]string, error) {
	if input == nil {
		return nil, nil
	}
	result := make(map[string]string, len(input))
	for id, value := range input {
		model := strings.TrimSpace(id)
		if model == "" || strings.Contains(model, "*") || strings.HasPrefix(model, "!") || len([]rune(model)) > 256 || strings.IndexFunc(model, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("invalid Claude Desktop model ID")
		}
		if _, exists := result[model]; exists {
			return nil, fmt.Errorf("duplicate Claude Desktop model ID")
		}
		tier := NormalizeClaudeDesktopTier(value)
		if strings.TrimSpace(value) != "" && tier == "" {
			return nil, fmt.Errorf("invalid Claude Desktop model tier")
		}
		result[model] = tier
	}
	return result, nil
}

func CloneClaudeDesktopModelTiers(input map[string]string) map[string]string {
	return maps.Clone(input)
}

// Existing capability overrides remain visible without rewriting their numeric
// fields. Dedicated values, including explicit clears, take precedence.
func ClaudeDesktopModelTiersForView(upstream UpstreamConfig) map[string]string {
	result := make(map[string]string)
	for model, capability := range upstream.ModelCapabilities {
		if strings.Contains(model, "*") || strings.HasPrefix(model, "!") {
			continue
		}
		if tier := NormalizeClaudeDesktopTier(capability.AnthropicFamilyTier); tier != "" {
			result[model] = tier
		}
	}
	for model, tier := range upstream.ClaudeDesktopModelTiers {
		result[model] = tier
	}
	return result
}

// ClaudeDesktopTierRulesForView exposes legacy matching rules separately from
// writable model IDs. Empty exact entries still block a less specific pattern.
func ClaudeDesktopTierRulesForView(upstream UpstreamConfig) map[string]string {
	rules := make(map[string]string)
	for pattern, capability := range upstream.ModelCapabilities {
		if (strings.Contains(pattern, "*") || strings.HasPrefix(pattern, "!")) && !isValidSupportedModelPattern(pattern) {
			continue
		}
		rules[pattern] = NormalizeClaudeDesktopTier(capability.AnthropicFamilyTier)
	}
	return rules
}
