package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentityProxyRevocationsPersistAndPolicyEditsCannotClearThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"upstream":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cm, err := NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	cfg := IdentityProxyConfig{Enabled: true, Issuer: "issuer", AllowedOrigins: []string{"https://console.example"}, TrustedCIDRs: []string{"127.0.0.1/32"}, GroupMappings: []IdentityGroupMapping{{Group: "team", Role: "viewer", ChannelUIDs: []string{"owned"}}}}
	if err := cm.SetIdentityProxyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	hash := IdentitySessionHash(cfg.Issuer, "sensitive-session-id")
	if err := cm.RevokeIdentitySession(strings.ToUpper(hash)); err != nil {
		t.Fatal(err)
	}
	if err := cm.RevokeIdentitySession(hash); err != nil {
		t.Fatal(err)
	}
	if err := cm.SetIdentityProxyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	snapshot := cm.IdentityProxySnapshot()
	if len(snapshot.RevokedSessionHashes) != 1 || snapshot.RevokedSessionHashes[0] != hash {
		t.Fatal("revocation lost or duplicated")
	}
	snapshot.GroupMappings[0].ChannelUIDs[0] = "other"
	if cm.IdentityProxySnapshot().GroupMappings[0].ChannelUIDs[0] != "owned" {
		t.Fatal("policy snapshot aliases live state")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sensitive-session-id") {
		t.Fatal("raw session persisted")
	}
	reload, err := NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer reload.Close()
	if got := reload.IdentityProxySnapshot(); len(got.RevokedSessionHashes) != 1 || got.RevokedSessionHashes[0] != hash {
		t.Fatal("revocation lost on restart")
	}
	for _, origin := range []string{"*", "https://*.example", "https://example/path", "file:///tmp/x", "https://user:secret@example", "null"} {
		if _, err := NormalizeIdentityOrigin(origin); err == nil {
			t.Fatalf("unsafe origin accepted: %s", origin)
		}
	}
}
