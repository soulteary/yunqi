package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEntryKeyPolicyPersistenceRotationAndSnapshotIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"upstream":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cm, err := NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	old := EntryKey{ID: "old", KeyHash: EntryKeyHash("secret-old"), PrincipalID: "team", Models: []string{"claude-*"}, CIDRs: []string{"192.0.2.0/24"}}
	if err := cm.PutEntryKey(old, ""); err != nil {
		t.Fatal(err)
	}
	snapshot := cm.EntryKeysSnapshot()
	snapshot[0].Models[0] = "*"
	actual, ok := cm.FindEntryKey("secret-old")
	if !ok || actual.Models[0] != "claude-*" {
		t.Fatal("policy snapshot aliases active configuration")
	}
	next := old
	next.ID = "new"
	next.KeyHash = EntryKeyHash("secret-new")
	if err := cm.PutEntryKey(next, "old"); err != nil {
		t.Fatal(err)
	}
	oldActual, _ := cm.FindEntryKey("secret-old")
	if oldActual.ValidNow(time.Now()) {
		t.Fatal("rotated credential stayed enabled")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-old") || strings.Contains(string(raw), "secret-new") {
		t.Fatal("raw gateway secret persisted")
	}
	reloaded, err := NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	got, ok := reloaded.FindEntryKey("secret-new")
	if !ok || got.PrincipalID != "team" || !got.ValidNow(time.Now()) {
		t.Fatalf("reload lost key: %+v", got)
	}
	if got.AllowsModel("gpt-x") || !got.AllowsModel("claude-sonnet") || got.AllowsIP("198.51.100.3") || !got.AllowsIP("192.0.2.3") {
		t.Fatal("ACL evaluation failed")
	}
	invalid := got
	invalid.ID = "invalid"
	invalid.KeyHash = EntryKeyHash("invalid")
	invalid.CIDRs = []string{"broken"}
	if cm.PutEntryKey(invalid, "") == nil {
		t.Fatal("invalid CIDR accepted")
	}
}
