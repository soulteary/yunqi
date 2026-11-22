package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClaudeDesktopTiersPersistWithoutCapabilityOverrides(t *testing.T) {
	cm := setupTestConfigManager(t)
	initial := cm.GetConfig()
	before := ResolveUpstreamCapability("deepseek-v4-pro", &initial.Upstream[0], nil)
	tiers := map[string]string{"deepseek-v4-pro": "sonnet", "deepseek-flash": "haiku"}
	if _, err := cm.UpdateUpstream(0, UpstreamUpdate{ClaudeDesktopModelTiers: tiers}); err != nil {
		t.Fatal(err)
	}
	tiers["deepseek-flash"] = "opus"
	cfg := cm.GetConfig()
	if cfg.Upstream[0].ClaudeDesktopModelTiers["deepseek-flash"] != "haiku" {
		t.Fatal("update aliases caller map")
	}
	after := ResolveUpstreamCapability("deepseek-v4-pro", &cfg.Upstream[0], nil)
	if !reflect.DeepEqual(before.Capability, after.Capability) || len(cfg.Upstream[0].ModelCapabilities) != 0 {
		t.Fatal("discovery mapping changed model capabilities")
	}
	cfg.Upstream[0].ClaudeDesktopModelTiers["deepseek-flash"] = "opus"
	if cm.GetConfig().Upstream[0].ClaudeDesktopModelTiers["deepseek-flash"] != "haiku" {
		t.Fatal("snapshot aliases live map")
	}
	data, err := json.Marshal(cm.GetConfig())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.GetConfig().Upstream[0].ClaudeDesktopModelTiers["deepseek-v4-pro"] != "sonnet" {
		t.Fatal("mapping lost on reload")
	}
	if _, err := cm.UpdateUpstream(0, UpstreamUpdate{}); err != nil {
		t.Fatal(err)
	}
	if len(cm.GetConfig().Upstream[0].ClaudeDesktopModelTiers) != 2 {
		t.Fatal("omission cleared mappings")
	}
	if _, err := cm.UpdateUpstream(0, UpstreamUpdate{ClaudeDesktopModelTiers: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if len(cm.GetConfig().Upstream[0].ClaudeDesktopModelTiers) != 0 {
		t.Fatal("empty update did not clear mappings")
	}
}

func TestClaudeDesktopTierValidation(t *testing.T) {
	for _, m := range []map[string]string{{"": "sonnet"}, {"model\nembedded": "sonnet"}, {"model": "invalid"}, {"a": "sonnet", " a": "haiku"}} {
		if _, err := NormalizeClaudeDesktopModelTiers(m); err == nil {
			t.Fatalf("accepted invalid mapping: %#v", m)
		}
	}
	got, err := NormalizeClaudeDesktopModelTiers(map[string]string{"model": " HAIKU ", "clear": ""})
	if err != nil || got["model"] != "haiku" || got["clear"] != "" {
		t.Fatalf("normalization = %#v, %v", got, err)
	}
	cm := setupTestConfigManager(t)
	if _, err := cm.UpdateUpstream(0, UpstreamUpdate{ClaudeDesktopModelTiers: map[string]string{"model": "invalid"}}); err == nil {
		t.Fatal("invalid update accepted")
	}
	if len(cm.GetConfig().Upstream[0].ClaudeDesktopModelTiers) != 0 {
		t.Fatal("invalid update changed state")
	}
}

func TestClaudeDesktopViewPreservesLegacyTierAndExplicitClear(t *testing.T) {
	up := UpstreamConfig{ModelCapabilities: map[string]UpstreamModelCapability{"deepseek-v4-pro": {AnthropicFamilyTier: "sonnet", ContextWindowTokens: 1048576}}}
	if ClaudeDesktopModelTiersForView(up)["deepseek-v4-pro"] != "sonnet" {
		t.Fatal("legacy tier missing from editor view")
	}
	up.ClaudeDesktopModelTiers = map[string]string{"deepseek-v4-pro": ""}
	view := ClaudeDesktopModelTiersForView(up)
	if value, present := view["deepseek-v4-pro"]; !present || value != "" {
		t.Fatal("explicit clear lost in editor view")
	}
	view["deepseek-v4-pro"] = "opus"
	if up.ClaudeDesktopModelTiers["deepseek-v4-pro"] != "" || up.ModelCapabilities["deepseek-v4-pro"].ContextWindowTokens != 1048576 {
		t.Fatal("view aliases live configuration")
	}
}

func TestClaudeDesktopLegacyPatternsAreReadOnlyRules(t *testing.T) {
	up := UpstreamConfig{ModelCapabilities: map[string]UpstreamModelCapability{"deepseek-*": {AnthropicFamilyTier: "sonnet"}, "deepseek-v4-pro": {AnthropicFamilyTier: "opus"}}}
	if _, present := ClaudeDesktopModelTiersForView(up)["deepseek-*"]; present {
		t.Fatal("pattern exposed as editable model ID")
	}
	if ClaudeDesktopTierRulesForView(up)["deepseek-*"] != "sonnet" {
		t.Fatal("legacy rule lost")
	}
	if _, err := NormalizeClaudeDesktopModelTiers(map[string]string{"deepseek-*": "haiku"}); err == nil {
		t.Fatal("accepted a pattern as an exact model ID")
	}
}
