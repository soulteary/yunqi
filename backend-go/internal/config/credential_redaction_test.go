package config

import (
	"path/filepath"
	"testing"

	"github.com/soulteary/yunqi/internal/redaction"
)

func TestSuccessfulChannelSaveObservesNewCredentialsBeforeReturning(t *testing.T) {
	redaction.Configure(nil)
	defer redaction.Configure(nil)
	cm := &ConfigManager{configFile: filepath.Join(t.TempDir(), "config.json"), config: Config{ChatUpstream: []UpstreamConfig{{ChannelUID: "route", ServiceType: "openai", BaseURL: "https://example.com", APIKeys: []string{"old-local"}}}}}
	const key = "unsaved!vendor-candidate=opaque"
	if _, err := cm.UpdateChatUpstream(0, UpstreamUpdate{APIKeys: []string{key}}); err != nil {
		t.Fatal(err)
	}
	if redaction.String(key) != redaction.Mask {
		t.Fatal("successful save returned before credential was protected")
	}
}

func TestFailedChannelSaveDoesNotRegisterUnappliedCredential(t *testing.T) {
	redaction.Configure(nil)
	defer redaction.Configure(nil)
	cm := &ConfigManager{configFile: filepath.Join(t.TempDir(), "missing", "config.json"), config: Config{ChatUpstream: []UpstreamConfig{{ChannelUID: "route", ServiceType: "openai", BaseURL: "https://example.com", APIKeys: []string{"old-local"}}}}}
	const key = "failed!vendor-candidate=opaque"
	if _, err := cm.UpdateChatUpstream(0, UpstreamUpdate{APIKeys: []string{key}}); err == nil {
		t.Fatal("expected persistence failure")
	}
	if redaction.String(key) != key {
		t.Fatal("unapplied configuration was published to the credential observer")
	}
}
