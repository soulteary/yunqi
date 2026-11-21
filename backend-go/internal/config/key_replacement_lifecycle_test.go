package config

import (
	"testing"
	"time"
)

func TestKeyReplacementLifecycle(t *testing.T) {
	for _, tt := range []struct {
		name           string
		old, next      []string
		source, status string
		reset, active  bool
	}{
		{"replace all", []string{"old-a", "old-b"}, []string{"new-a", "new-b"}, "", "active", true, true},
		{"partial retention", []string{"old-a", "old-b"}, []string{"old-b", "new"}, "", "active", false, true},
		{"reorder", []string{"a", "b"}, []string{"b", "a"}, "", "active", false, true},
		{"manual suspension", []string{"old"}, []string{"new"}, SuspensionSourceManual, "suspended", true, false},
		{"automatic suspension", nil, []string{"new"}, SuspensionSourceAutoNoKeys, "suspended", true, true},
		{"clear keys", []string{"old"}, []string{}, "", "active", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ch := UpstreamConfig{APIKeys: tt.old, Status: tt.status, SuspensionSource: tt.source,
				DisabledAPIKeys: []DisabledKeyInfo{{Key: "old-a"}}}
			reset, err := applyUpstreamUpdateFields(&ch, UpstreamUpdate{APIKeys: tt.next})
			if err != nil {
				t.Fatal(err)
			}
			if reset != tt.reset {
				t.Fatalf("reset=%v want %v", reset, tt.reset)
			}
			if (ch.Status == "active") != tt.active {
				t.Fatalf("status=%q source=%q", ch.Status, ch.SuspensionSource)
			}
			for _, key := range tt.next {
				if key != "old-a" && ch.IsKeyDisabledNow(key, time.Now()) {
					t.Fatalf("new key inherited blacklist: %q", key)
				}
			}
		})
	}
}

func TestKeyReplacementDoesNotShareDisableStateAcrossAccounts(t *testing.T) {
	a := UpstreamConfig{AccountUID: "account-a", APIKeys: []string{"same"}, DisabledAPIKeys: []DisabledKeyInfo{{Key: "same"}}}
	b := UpstreamConfig{AccountUID: "account-b", APIKeys: []string{"other"}}
	if _, err := applyUpstreamUpdateFields(&b, UpstreamUpdate{APIKeys: []string{"same"}}); err != nil {
		t.Fatal(err)
	}
	if !a.IsKeyDisabledNow("same", time.Now()) || b.IsKeyDisabledNow("same", time.Now()) {
		t.Fatal("account disable state leaked")
	}
}
