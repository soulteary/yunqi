package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminCredentialsPersistOnlyDigestsAndKeepScopeSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"upstream":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cm, err := NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	key := AdminCredential{ID: "credential", ActorID: "operator", Role: "operator", KeyHash: EntryKeyHash("management-secret"), ChannelUIDs: []string{"owned"}}
	if err := cm.PutAdminCredential(key, ""); err != nil {
		t.Fatal(err)
	}
	view := cm.AdminCredentialsSnapshot()
	view[0].ChannelUIDs[0] = "other"
	stored, ok := cm.FindAdminCredential("management-secret")
	if !ok || stored.ChannelUIDs[0] != "owned" {
		t.Fatal("scope snapshot aliases live state")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "management-secret") {
		t.Fatal("raw management key persisted")
	}
	reload, err := NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer reload.Close()
	stored, ok = reload.FindAdminCredential("management-secret")
	if !ok || stored.ActorID != "operator" || stored.Role != "operator" {
		t.Fatal("management identity lost on reload")
	}
	key.Role = "owner"
	if err := cm.PutAdminCredential(key, ""); err == nil {
		t.Fatal("unknown role accepted")
	}
}
