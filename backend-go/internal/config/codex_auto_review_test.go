package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexAutoReviewMappingPersistenceAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"upstream":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if manager.GetCodexAutoReviewMapping().Enabled {
		t.Fatal("mapping must be opt-in")
	}
	want := CodexAutoReviewMapping{Enabled: true, TargetModel: "review-model"}
	if err := manager.SetCodexAutoReviewMapping(CodexAutoReviewMapping{Enabled: true, TargetModel: " review-model "}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"", CodexAutoReviewModel, "CODEX-AUTO-REVIEW", "bad\x00model", strings.Repeat("a", 257)} {
		if err := manager.SetCodexAutoReviewMapping(CodexAutoReviewMapping{Enabled: true, TargetModel: target}); err == nil {
			t.Fatalf("accepted invalid target %q", target)
		}
		if got := manager.GetCodexAutoReviewMapping(); got != want {
			t.Fatalf("invalid update changed live mapping: %#v", got)
		}
	}
	manager.Close()
	manager, err = NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if got := manager.GetCodexAutoReviewMapping(); got != want {
		t.Fatalf("mapping lost on reload: %#v", got)
	}
	want.Enabled = false
	if err := manager.SetCodexAutoReviewMapping(want); err != nil {
		t.Fatal(err)
	}
	if got := manager.GetCodexAutoReviewMapping(); got != want {
		t.Fatalf("disable lost target: %#v", got)
	}
	// A failed disk write must not activate an in-memory-only mapping.
	// Stop this temporary manager's watcher before changing its storage path.
	manager.Close()
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	previousPath := manager.configFile
	manager.configFile = filepath.Join(blocker, "config.json")
	manager.mu.Unlock()
	if err := manager.SetCodexAutoReviewMapping(CodexAutoReviewMapping{Enabled: true, TargetModel: "unsaved-model"}); err == nil {
		t.Fatal("expected write failure")
	}
	if got := manager.GetCodexAutoReviewMapping(); got != want {
		t.Fatalf("failed write activated mapping: %#v", got)
	}
	manager.mu.Lock()
	manager.configFile = previousPath
	manager.mu.Unlock()
}
