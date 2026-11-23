package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestClientMappingsSharedTargetsPersistAndClear(t *testing.T) {
	cm := setupTestConfigManager(t)
	models := []string{"deepseek-flash", "deepseek-v4-pro"}
	if _, err := cm.UpdateUpstream(0, UpstreamUpdate{SupportedModels: models}); err != nil {
		t.Fatal(err)
	}
	mappings := ClientModelMappings{"claude-desktop": {}}
	for _, slot := range clientMappingSlots["claude-desktop"] {
		mappings["claude-desktop"][slot] = "deepseek-v4-pro"
	}
	before := cm.GetConfig().Upstream[0].ModelCapabilities
	if err := cm.SetClientModelMappingsAudited(ManagementAuditContext{}, mappings); err != nil {
		t.Fatal(err)
	}
	mappings["claude-desktop"]["haiku"] = "mutated"
	snapshot := cm.GetConfig()
	snapshot.ClientModelMappings["claude-desktop"]["opus"] = "mutated"
	for _, slot := range clientMappingSlots["claude-desktop"] {
		target, reserved := cm.ResolveClientModelAlias(ClientMappingAlias("claude-desktop", slot), "messages")
		if target != "deepseek-v4-pro" || !reserved {
			t.Fatalf("slot %s resolves to %q, %v", slot, target, reserved)
		}
	}
	if !reflect.DeepEqual(before, cm.GetConfig().Upstream[0].ModelCapabilities) {
		t.Fatal("mapping changed capabilities")
	}
	for _, clear := range []ClientModelMappings{{"claude-desktop": {}}, {}} {
		if err := cm.SetClientModelMappingsAudited(ManagementAuditContext{}, clear); err != nil {
			t.Fatal(err)
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
		if loaded.GetConfig().ClientModelMappings == nil || len(loaded.GetClientModelMappings()["claude-desktop"]) != 0 {
			t.Fatal("cleared mappings did not survive reload")
		}
		loaded.Close()
	}
}
func TestClientMappingsRejectInvalidAndUnavailableSelections(t *testing.T) {
	cm := setupTestConfigManager(t)
	models := []string{"deepseek-v4-pro"}
	if _, err := cm.UpdateUpstream(0, UpstreamUpdate{SupportedModels: models}); err != nil {
		t.Fatal(err)
	}
	for _, input := range []ClientModelMappings{
		nil, {"unknown": {"default": "deepseek-v4-pro"}}, {"claude-desktop": {"invalid": "deepseek-v4-pro"}},
		{"claude-desktop": {"sonnet": "unknown"}}, {"claude-desktop": {"sonnet": "yunqi-claude-desktop-opus"}},
		{"codex": {"default": "deepseek-v4-pro"}}, {"claude-code": {"haiku": "deepseek-*"}},
	} {
		if err := cm.SetClientModelMappingsAudited(ManagementAuditContext{}, input); err == nil {
			t.Fatalf("accepted %#v", input)
		}
	}
	if err := cm.SetClientModelMappingsAudited(ManagementAuditContext{}, ClientModelMappings{"claude-desktop": {"sonnet": "deepseek-v4-pro"}}); err != nil {
		t.Fatal(err)
	}
	if target, reserved := cm.ResolveClientModelAlias(ClientMappingAlias("claude-desktop", "sonnet"), "chat"); target != "" || !reserved {
		t.Fatal("cross-protocol mapping")
	}
	status := "suspended"
	if _, err := cm.UpdateUpstream(0, UpstreamUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}
	if len(cm.ClientMappingTargets()) != 0 {
		t.Fatal("inactive target offered")
	}
	if target, _ := cm.ResolveClientModelAlias(ClientMappingAlias("claude-desktop", "sonnet"), "messages"); target != "" {
		t.Fatal("inactive target resolved")
	}
	if err := cm.SetClientModelMappingsAudited(ManagementAuditContext{}, cm.GetClientModelMappings()); err != nil {
		t.Fatal("existing unavailable choice cannot be retained", err)
	}
}
func TestClientMappingInventoryPreservesRouteAndImportBoundaries(t *testing.T) {
	cfg := Config{Upstream: []UpstreamConfig{
		{Name: "active", Status: "active", APIKeys: []string{"test"}, SupportedModels: []string{"a", "b", "*", "!b"}},
		{Name: "prefixed", Status: "active", APIKeys: []string{"test"}, RoutePrefix: "private", SupportedModels: []string{"private"}},
		{Name: "suspended", Status: "suspended", APIKeys: []string{"test"}, SupportedModels: []string{"suspended"}},
		{Name: "keyless", Status: "active", SupportedModels: []string{"keyless"}},
		{Name: "imported", Status: "active", APIKeys: []string{"test"}, SupportedModels: []string{"approved-upstream"}, ModelImport: &ModelImportState{Policy: ModelImportPolicy{Enabled: true}, Approved: []ImportedModel{{ID: "approved", UpstreamID: "approved-upstream"}}}},
	}}
	got := clientMappingTargets(cfg)
	if len(got) != 2 || got[0].Model != "a" || got[1].Model != "approved" {
		t.Fatalf("inventory=%+v", got)
	}
	cfg.Upstream[0].ClaudeDesktopModelTiers = map[string]string{"a": "sonnet"}
	legacy := effectiveClientMappings(cfg)
	if legacy["claude-desktop"]["sonnet"] != "a" || cfg.ClientModelMappings != nil {
		t.Fatal("legacy projection changed persisted state")
	}
	cfg.ClientModelMappings = ClientModelMappings{}
	if len(effectiveClientMappings(cfg)) != 0 {
		t.Fatal("explicit clear inherited legacy mapping")
	}
}
