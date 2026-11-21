package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestChannelUpdateRollsBackValidationAndPersistenceFailures(t *testing.T) {
	for _, kind := range []string{"messages", "chat", "responses", "gemini", "images", "vectors"} {
		for _, failure := range []string{"validation", "persistence"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				k, err := ChannelKindByName(kind)
				if err != nil {
					t.Fatal(err)
				}
				cm := &ConfigManager{configFile: filepath.Join(t.TempDir(), "missing", "config.json")}
				*k.SliceRef(&cm.config) = []UpstreamConfig{{ChannelUID: "route", BaseURL: "https://old.example", ServiceType: k.DefaultServiceType, APIKeys: []string{"old"}, Description: "before"}}
				before := cm.config.deepCopy()
				base := "https://new.example"
				desc := "after"
				patch := UpstreamUpdate{BaseURL: &base, Description: &desc}
				if failure == "validation" {
					remark := strings.Repeat("a", remarkMaxRunes+1)
					patch.Remark = &remark
				}
				if _, err := cm.updateUpstreamCommon(k, 0, patch); err == nil {
					t.Fatal("expected failure")
				}
				if !reflect.DeepEqual(cm.config, before) {
					t.Fatal("failed save changed live configuration")
				}
				if _, err := os.Stat(cm.configFile); !os.IsNotExist(err) {
					t.Fatalf("failed save created file: %v", err)
				}
			})
		}
	}
}

func TestChannelSaveReceiptRevisionConflict(t *testing.T) {
	cm := &ConfigManager{configFile: filepath.Join(t.TempDir(), "config.json"), config: Config{ChatUpstream: []UpstreamConfig{{ChannelUID: "route", ServiceType: "openai", BaseURL: "https://example.com", APIKeys: []string{"secret-key"}}}}}
	initial := ChannelRevision(cm.config.ChatUpstream[0])
	desc := "saved"
	receipt, _, _, err := cm.UpdateChannelWithReceipt("route", initial, UpstreamUpdate{Description: &desc})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Persisted || !receipt.Changed || receipt.Revision == initial || strings.Contains(receipt.Revision, "secret") {
		t.Fatalf("invalid receipt: %+v", receipt)
	}
	desc = "stale"
	if _, _, _, err := cm.UpdateChannelWithReceipt("route", initial, UpstreamUpdate{Description: &desc}); err != ErrChannelRevisionConflict {
		t.Fatalf("stale revision error=%v", err)
	}
	if cm.config.ChatUpstream[0].Description != "saved" {
		t.Fatal("stale save mutated configuration")
	}
	desc = "saved"
	receipt, _, _, err = cm.UpdateChannelWithReceipt("route", receipt.Revision, UpstreamUpdate{Description: &desc})
	if err != nil || !receipt.Persisted || receipt.Changed {
		t.Fatalf("no-op receipt: %+v error=%v", receipt, err)
	}
}

func TestAtomicConfigWritePreservesOldFileOnRenameFailure(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.json")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(target, "original")
	if err := os.WriteFile(sentinel, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeConfigAtomically(target, []byte("new")); err == nil {
		t.Fatal("expected rename failure")
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "old" {
		t.Fatalf("old configuration lost: %q %v", data, err)
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".config-save-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary files leaked: %v %v", files, err)
	}
}
