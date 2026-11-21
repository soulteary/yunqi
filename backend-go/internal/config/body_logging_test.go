package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func resetBodyLoggingForTest(t *testing.T) {
	t.Helper()
	old := bodyLoggingRuntime.Load()
	bodyLoggingRuntime.Store(nil)
	t.Cleanup(func() { bodyLoggingRuntime.Store(old) })
}
func TestBodyLoggingDefaultsOffAndCommitsAuditBeforeActivation(t *testing.T) {
	resetBodyLoggingForTest(t)
	env := &EnvConfig{Env: "development", EnableRequestLogs: true, EnableResponseLogs: true}
	if env.ShouldLogRequestBodies() || env.ShouldLogResponseBodies() {
		t.Fatal("body logging enabled by default")
	}
	cm := &ConfigManager{configFile: filepath.Join(t.TempDir(), "config.json")}
	state, err := cm.UpdateBodyLogging("owner:stable-full-hash", "", BodyLoggingSettings{RequestBodies: true, MaxBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	if !env.ShouldLogRequestBodies() || env.ShouldLogResponseBodies() || len(state.Audit) != 1 || state.Audit[0].ActorID != "owner:stable-full-hash" || state.Audit[0].Before.RequestBodies {
		t.Fatalf("invalid activation: %+v", state)
	}
	data, err := os.ReadFile(cm.configFile)
	if err != nil {
		t.Fatal(err)
	}
	var persisted Config
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if !persisted.BodyLogging.RequestBodies || len(persisted.BodyLogAudit) != 1 {
		t.Fatal("activation was not persisted with its audit")
	}
	previous := state.Revision
	state, err = cm.UpdateBodyLogging("owner:stable-full-hash", state.Revision, BodyLoggingSettings{MaxBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	if env.ShouldLogRequestBodies() || len(state.Audit) != 2 || state.Revision == previous {
		t.Fatal("disable was not audited")
	}
	if _, err := cm.UpdateBodyLogging("owner", previous, BodyLoggingSettings{ResponseBodies: true}); err != ErrBodyLogRevisionConflict {
		t.Fatalf("stale revision accepted: %v", err)
	}
	clone := cm.GetConfig()
	clone.BodyLogAudit[0].ActorID = "modified"
	if cm.GetBodyLoggingState().Audit[0].ActorID == "modified" {
		t.Fatal("audit snapshot aliases configuration")
	}
}
func TestBodyLoggingFailureDoesNotEnableOrPublishAudit(t *testing.T) {
	resetBodyLoggingForTest(t)
	cm := &ConfigManager{configFile: filepath.Join(t.TempDir(), "missing", "config.json")}
	if _, err := cm.UpdateBodyLogging("actor", "", BodyLoggingSettings{RequestBodies: true}); err == nil {
		t.Fatal("expected persistence failure")
	}
	if RuntimeBodyLogging().RequestBodies || len(cm.config.BodyLogAudit) != 0 || cm.config.BodyLogging.RequestBodies {
		t.Fatal("failed save enabled content logging")
	}
	for _, test := range []struct {
		actor    string
		settings BodyLoggingSettings
	}{{"", BodyLoggingSettings{RequestBodies: true}}, {"actor", BodyLoggingSettings{MaxBytes: 1}}, {"actor", BodyLoggingSettings{MaxBytes: 65537}}} {
		if _, err := cm.UpdateBodyLogging(test.actor, "", test.settings); err == nil {
			t.Fatal("invalid body logging update accepted")
		}
	}
}
func TestConfiguredBodyLoggingRequiresStartupAudit(t *testing.T) {
	resetBodyLoggingForTest(t)
	cm := &ConfigManager{configFile: filepath.Join(t.TempDir(), "config.json"), config: Config{BodyLogging: BodyLoggingSettings{ResponseBodies: true}}}
	state, err := cm.ActivateBodyLogging("system:startup")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Runtime.ResponseBodies || len(state.Audit) != 1 || state.Audit[0].ActorID != "system:startup" {
		t.Fatalf("startup activation missing audit: %+v", state)
	}
	bodyLoggingRuntime.Store(nil)
	cm.configFile = filepath.Join(t.TempDir(), "missing", "config.json")
	if _, err := cm.ActivateBodyLogging("system:config-reload"); err == nil || RuntimeBodyLogging().ResponseBodies {
		t.Fatal("reload enabled body logging without durable audit")
	}
}
func TestBodyLogAuditRetentionIsBounded(t *testing.T) {
	resetBodyLoggingForTest(t)
	cm := &ConfigManager{configFile: filepath.Join(t.TempDir(), "config.json")}
	for i := 0; i < MaxBodyLogAuditEntries; i++ {
		cm.config.BodyLogAudit = append(cm.config.BodyLogAudit, BodyLogAuditEvent{ID: fmt.Sprint(i), ActorID: "seed"})
	}
	state, err := cm.UpdateBodyLogging("latest-actor", "", BodyLoggingSettings{RequestBodies: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Audit) != MaxBodyLogAuditEntries || state.Audit[0].ID != "1" || state.Audit[len(state.Audit)-1].ActorID != "latest-actor" {
		t.Fatal("audit retention lost newest or exceeded bound")
	}
}
