package config

import (
	"errors"
	"testing"
)

func TestModelImportDiscoveryTransactionsBindSourceAndGovernedTargets(t *testing.T) {
	cm := newImportManager(t)
	cm.config.ChatUpstream[0].AccountUID = "account"
	source := cm.GetConfig().ChatUpstream[0]
	if err := cm.UpdateDiscoveryChannel(&source, source.ChannelUID, UpstreamUpdate{SupportedModels: []string{"raw"}}); err != nil {
		t.Fatal(err)
	}
	fingerprint := true
	if err := cm.UpdateDiscoveryChannel(&source, source.ChannelUID, UpstreamUpdate{LearnedClientFingerprint: &fingerprint}); err != nil {
		t.Fatalf("own successful snapshot not advanced: %v", err)
	}
	sibling := source.Clone()
	sibling.ChannelUID = "sibling"
	sibling.ModelImport = &ModelImportState{Policy: ModelImportPolicy{Enabled: false}}
	cm.config.ChatUpstream = append(cm.config.ChatUpstream, *sibling)
	status := "disabled"
	if err := cm.UpdateDiscoveryChannel(&source, "sibling", UpstreamUpdate{Status: &status}); !errors.Is(err, ErrModelImportConflict) {
		t.Fatalf("governed sibling changed: %v", err)
	}
	if err := cm.ApplyDiscoveryAccountChannelChanges(&source, "account", []AccountChannelUpdate{{ChannelUID: "sibling"}}, nil); !errors.Is(err, ErrModelImportConflict) {
		t.Fatalf("governed account target changed: %v", err)
	}
	if err := cm.ApplyDiscoveryAccountChannelChanges(&source, "other-account", nil, nil); !errors.Is(err, ErrModelImportConflict) {
		t.Fatalf("different account trusted source: %v", err)
	}
	view, _ := cm.GetModelImport(source.ChannelUID)
	if _, err := cm.SetModelImportPolicy(source.ChannelUID, view.Revision, ModelImportPolicy{Enabled: false}, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := cm.UpdateDiscoveryChannel(&source, source.ChannelUID, UpstreamUpdate{Status: &status}); !errors.Is(err, ErrModelImportConflict) {
		t.Fatalf("new draft ignored source revision: %v", err)
	}
	if got := cm.GetConfig().ChatUpstream[0]; got.Status == "disabled" || got.ModelImport == nil {
		t.Fatal("stale discovery modified draft")
	}
}
