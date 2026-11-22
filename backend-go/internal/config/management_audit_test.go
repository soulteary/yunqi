package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func auditTestManager(t *testing.T) *ConfigManager {
	t.Helper()
	cm, err := NewConfigManager(filepath.Join(t.TempDir(), "config.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cm.Close() })
	return cm
}
func auditTestContext(id string) ManagementAuditContext {
	return ManagementAuditContext{OperationID: "op_" + id, ActorID: "admin:" + id, Action: "test.update", Resource: "config:test"}
}
func auditEventFor(t *testing.T, cm *ConfigManager, operation string) ManagementAuditEvent {
	t.Helper()
	for _, event := range cm.ManagementAuditSnapshot().Events {
		if event.OperationID == operation && event.Outcome == "committed" {
			return event
		}
	}
	t.Fatal("missing committed operation", operation)
	return ManagementAuditEvent{}
}
func TestManagementAuditCommitsWithConfigurationAndSurvivesRestart(t *testing.T) {
	cm := auditTestManager(t)
	audit := auditTestContext("first")
	if err := cm.SetRacingEnabledAudited(audit, true); err != nil {
		t.Fatal(err)
	}
	event := auditEventFor(t, cm, audit.OperationID)
	if event.ActorID != audit.ActorID || event.BeforeDigest == event.AfterDigest || len(event.Differences) == 0 {
		t.Fatal("missing committed metadata or field differences")
	}
	data, err := os.ReadFile(cm.configFile)
	if err != nil {
		t.Fatal(err)
	}
	var disk Config
	if json.Unmarshal(data, &disk) != nil || disk.ManagementAudit == nil {
		t.Fatal("configuration published without audit")
	}
	encoded, _ := json.Marshal(disk.ManagementAudit)
	if !strings.Contains(string(encoded), audit.OperationID) {
		t.Fatal("audit not in same persisted configuration")
	}
	if cm.GetConfig().ManagementAudit != nil {
		t.Fatal("hot-path snapshot copied historical audit")
	}
	snapshot := cm.ManagementAuditSnapshot()
	snapshot.Events[0].ActorID = "mutated-reader"
	if cm.ManagementAuditSnapshot().Events[0].ActorID == "mutated-reader" {
		t.Fatal("audit snapshot aliased authoritative state")
	}
	if err = cm.loadConfig(); err != nil {
		t.Fatal(err)
	}
	_ = auditEventFor(t, cm, audit.OperationID)
}
func TestManagementAuditFailureCannotPublishStateOrSuccess(t *testing.T) {
	cm := auditTestManager(t)
	if err := cm.SetRacingEnabledAudited(auditTestContext("before"), true); err != nil {
		t.Fatal(err)
	}
	originalPath := cm.configFile
	originalData, _ := os.ReadFile(originalPath)
	state := cm.ManagementAuditSnapshot()
	cm.configFile = t.TempDir() // read/write fails, even as a privileged CI user
	err := cm.SetRacingEnabledAudited(auditTestContext("failed"), false)
	cm.configFile = originalPath
	if err == nil {
		t.Fatal("failed persistence reported success")
	}
	if !cm.GetRacingEnabled() {
		t.Fatal("failed audited save published in-memory change")
	}
	if cm.ManagementAuditSnapshot().NextSequence != state.NextSequence {
		t.Fatal("failed save published success audit")
	}
	now, _ := os.ReadFile(originalPath)
	if string(now) != string(originalData) {
		t.Fatal("failed save changed disk state")
	}
}
func TestManagementAuditCandidateCannotEraseOrForgeHistory(t *testing.T) {
	cm := auditTestManager(t)
	if err := cm.SetRacingEnabledAudited(auditTestContext("one"), true); err != nil {
		t.Fatal(err)
	}
	before := cm.ManagementAuditSnapshot()
	candidate := cm.GetConfig()
	disabled := false
	candidate.Racing = &GlobalRacingConfig{Enabled: &disabled}
	candidate.ManagementAudit = &ManagementAuditState{NextSequence: 99999, Events: []ManagementAuditEvent{{ManagementAuditContext: ManagementAuditContext{ActorID: "forged-actor"}}}}
	cm.mu.Lock()
	err := cm.saveConfigLockedAudited(auditTestContext("import"), candidate)
	cm.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	_ = auditEventFor(t, cm, "op_one")
	_ = auditEventFor(t, cm, "op_import")
	after := cm.ManagementAuditSnapshot()
	if after.NextSequence != before.NextSequence+1 {
		t.Fatal("candidate reset or forged sequence")
	}
	encoded, _ := json.Marshal(after)
	if strings.Contains(string(encoded), "forged-actor") {
		t.Fatal("candidate forged authoritative audit")
	}
	if err = cm.SetOverrideTTLMinutesAudited(auditTestContext("third"), 123); err != nil {
		t.Fatal(err)
	}
	_ = auditEventFor(t, cm, "op_one")
	_ = auditEventFor(t, cm, "op_import")
	_ = auditEventFor(t, cm, "op_third")
}
func TestManagementAuditDiffDoesNotExposeValuesOrDynamicMapKeys(t *testing.T) {
	cm := auditTestManager(t)
	audit := auditTestContext("redaction")
	secrets := []string{"never-log-this-api-secret", "private-channel-name", "Odd-New-Password-Header", "opaque-header-credential", "private-custom-model"}
	upstream := UpstreamConfig{Name: secrets[1], BaseURL: "https://provider.example", APIKeys: []string{secrets[0]}, ServiceType: "openai", Status: "active", CustomHeaders: map[string]string{secrets[2]: secrets[3]}, SupportedModels: []string{secrets[4]}}
	if err := cm.AddChatUpstreamAudited(audit, upstream); err != nil {
		t.Fatal(err)
	}
	event := auditEventFor(t, cm, audit.OperationID)
	encoded, _ := json.Marshal(event)
	for _, secret := range secrets {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("audit leaked %q", secret)
		}
	}
	if !strings.Contains(string(encoded), "sha256:") {
		t.Fatal("no redacted summaries")
	}
}
func TestManagementAuditConcurrentSystemAndActorsDoNotCrossAttribute(t *testing.T) {
	cm := auditTestManager(t)
	if err := cm.AddChatUpstream(UpstreamConfig{BaseURL: "https://audit.example", APIKeys: []string{"synthetic-test-key"}, ServiceType: "openai", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 20)
	for i := 0; i < 10; i++ {
		i := i
		wg.Add(2)
		go func() {
			defer wg.Done()
			description := "actor-description-" + strconv.Itoa(i)
			_, err := cm.UpdateChatUpstreamAudited(auditTestContext(strconv.Itoa(i)), 0, UpstreamUpdate{Description: &description})
			failures <- err
		}()
		go func() {
			defer wg.Done()
			description := "system-description-" + strconv.Itoa(i)
			_, err := cm.UpdateChatUpstream(0, UpstreamUpdate{Description: &description})
			failures <- err
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 10; i++ {
		ctx := auditTestContext(strconv.Itoa(i))
		event := auditEventFor(t, cm, ctx.OperationID)
		if event.ActorID != ctx.ActorID {
			t.Fatalf("actor crossed operation %s", ctx.OperationID)
		}
	}
	system := 0
	for _, event := range cm.ManagementAuditSnapshot().Events {
		if event.ActorID == "system" && event.Outcome == "committed" {
			system++
		}
	}
	if system < 10 {
		t.Fatal("background changes inherited an HTTP actor")
	}
}
func TestManagementAuditRetentionIsBoundedAndMonotonic(t *testing.T) {
	state := &ManagementAuditState{NextSequence: 1005, Events: make([]ManagementAuditEvent, 1005)}
	for i := range state.Events {
		state.Events[i].Sequence = uint64(i + 1)
	}
	state = appendManagementAudit(state, ManagementAuditEvent{Outcome: "committed"})
	if len(state.Events) != ManagementAuditMaxEntries || state.NextSequence != 1006 || state.Events[0].Sequence != 7 {
		t.Fatal("entry retention or sequence is incorrect")
	}
	diffs := make([]ManagementAuditDiff, managementAuditMaxDiffs)
	for i := range diffs {
		diffs[i] = ManagementAuditDiff{Path: strings.Repeat("p", 256), Before: strings.Repeat("h", 100), After: strings.Repeat("h", 100)}
	}
	state = &ManagementAuditState{NextSequence: 100, Events: make([]ManagementAuditEvent, 100)}
	for i := range state.Events {
		state.Events[i] = ManagementAuditEvent{Sequence: uint64(i + 1), Differences: diffs}
	}
	state = appendManagementAudit(state, ManagementAuditEvent{Outcome: "committed"})
	data, _ := json.Marshal(state)
	if len(data) > ManagementAuditMaxBytes || len(state.Events) >= 100 || state.NextSequence != 101 {
		t.Fatal("byte retention is not bounded")
	}
}

func TestManagementAuditHotReloadCannotImportForgedHistory(t *testing.T) {
	cm := auditTestManager(t)
	if err := cm.SetRacingEnabledAudited(auditTestContext("authoritative"), true); err != nil {
		t.Fatal(err)
	}
	candidate := cm.GetConfig()
	candidate.ManagementAudit = &ManagementAuditState{NextSequence: 999999, Events: []ManagementAuditEvent{{ManagementAuditContext: ManagementAuditContext{ActorID: "forged-import"}}}}
	data, _ := json.Marshal(candidate)
	if err := os.WriteFile(cm.configFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := cm.loadConfig(); err != nil {
		t.Fatal(err)
	}
	_ = auditEventFor(t, cm, "op_authoritative")
	disk, err := os.ReadFile(cm.configFile)
	if err != nil {
		t.Fatal(err)
	}
	var restored Config
	if json.Unmarshal(disk, &restored) != nil {
		t.Fatal("invalid restored config")
	}
	audit, _ := json.Marshal(restored.ManagementAudit)
	if strings.Contains(string(audit), "forged-import") || !strings.Contains(string(audit), "op_authoritative") {
		t.Fatal("hot reload forged or erased authoritative audit")
	}
}

func TestManagementAuditPendingExternalACLIsNotMarkedAsSelfWrite(t *testing.T) {
	cm := auditTestManager(t)
	if err := cm.PutAdminCredential(AdminCredential{ID: "reader", KeyHash: EntryKeyHash("reader-key"), ActorID: "reader", Role: "viewer"}, ""); err != nil {
		t.Fatal(err)
	}
	// Stop only this fixture's filesystem watcher, then exercise its exact reload
	// function explicitly to make the edit→intent→reload ordering deterministic.
	if cm.watcher != nil {
		_ = cm.watcher.Close()
	}
	data, err := os.ReadFile(cm.configFile)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	_ = json.Unmarshal(data, &document)
	var keys []AdminCredential
	_ = json.Unmarshal(document["adminCredentials"], &keys)
	keys[0].Disabled = true
	document["adminCredentials"], _ = json.Marshal(keys)
	external, _ := json.MarshalIndent(document, "", "  ")
	if err = os.WriteFile(cm.configFile, external, 0600); err != nil {
		t.Fatal(err)
	}
	blocked := auditTestContext("blocked")
	if err = cm.RecordManagementRequest(blocked, "intent", 0); !errors.Is(err, ErrManagementAuditReloadPending) {
		t.Fatal("pending external ACL was accepted as audit-only write", err)
	}
	if cm.skipReloadIfSelfWritten() {
		t.Fatal("external ACL was marked self-written and would be swallowed")
	}
	if err = cm.SetRacingEnabledAudited(blocked, true); !errors.Is(err, ErrManagementAuditReloadPending) {
		t.Fatal("ordinary write overwrote a pending external edit", err)
	}
	unchanged, _ := os.ReadFile(cm.configFile)
	if string(unchanged) != string(external) {
		t.Fatal("blocked intent or save overwrote external ACL")
	}
	if err = cm.loadConfig(); err != nil {
		t.Fatal(err)
	}
	credential, ok := cm.FindAdminCredential("reader-key")
	if !ok || !credential.Disabled {
		t.Fatal("watcher reload did not publish edited ACL")
	}
	reloaded := false
	for _, event := range cm.ManagementAuditSnapshot().Events {
		if event.OperationID == blocked.OperationID {
			t.Fatal("external edit was attributed to blocked HTTP actor")
		}
		if event.Action == "config.reload" && event.Outcome == "committed" {
			for _, diff := range event.Differences {
				if strings.Contains(diff.Path, "disabled") {
					reloaded = true
					if event.ActorID != "system" {
						t.Fatal("external edit inherited an HTTP actor")
					}
				}
			}
		}
	}
	if !reloaded {
		t.Fatal("external ACL publication was not atomically audited")
	}
	if err = cm.RecordManagementRequest(auditTestContext("after-reload"), "intent", 0); err != nil {
		t.Fatal("successful reload did not release audit admission", err)
	}
}
func TestManagementAuditRawDigestPreservesUnknownNumberPrecision(t *testing.T) {
	before := []byte(`{"futureField":{"large":9007199254740992},"known":true}`)
	after := []byte(`{"futureField":{"large":9007199254740993},"known":true}`)
	cm := &ConfigManager{}
	cm.recordAuditPublishedDocument(before)
	if err := cm.checkAuditPublishedDocument(after); !errors.Is(err, ErrManagementAuditReloadPending) {
		t.Fatal("adjacent integers beyond 2^53 collided in business digest", err)
	}
	same := []byte("{\n \"known\":true, \"futureField\":{\"large\":9007199254740992}}")
	if err := cm.checkAuditPublishedDocument(same); err != nil {
		t.Fatal("unknown fields or JSON formatting caused false pending reload", err)
	}
}

func TestManagementAuditOnlyPreservesBusinessSnapshotAndRevisions(t *testing.T) {
	cm := auditTestManager(t)
	if err := cm.AddChatUpstream(UpstreamConfig{BaseURL: "https://preview.example", APIKeys: []string{"synthetic-preview-key"}, ServiceType: "openai", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(cm.GetConfig())
	_, _, revisions := cm.ChannelSaveViews()
	bodyRevision := cm.GetBodyLoggingState().Revision
	callbacks := 0
	cm.RegisterOnConfigChange(func(Config) { callbacks++ })
	audit := auditTestContext("read-only-preview")
	if err := cm.RecordManagementRequest(audit, "intent", 0); err != nil {
		t.Fatal(err)
	}
	if err := cm.RecordManagementRequest(audit, "completed", 200); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(cm.GetConfig())
	_, _, afterRevisions := cm.ChannelSaveViews()
	left, _ := json.Marshal(revisions)
	right, _ := json.Marshal(afterRevisions)
	if string(before) != string(after) || string(left) != string(right) || cm.GetBodyLoggingState().Revision != bodyRevision || callbacks != 0 {
		t.Fatal("audit-only metadata changed business config, revision or reload callbacks")
	}
}

func TestManagementAuditLogicalTransactionsRollbackOnce(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(strconv.FormatBool(existing), func(t *testing.T) {
			cm := auditTestManager(t)
			input := CreateLogicalChannelInput{Name: "first", BaseURLs: []string{"https://first.example"}, Protocols: []CreateLogicalChannelProtocol{{Kind: "messages", ServiceType: "claude", APIKeys: []string{"test"}}}}
			var uid string
			if existing {
				created, err := cm.CreateLogicalChannel(input)
				if err != nil {
					t.Fatal(err)
				}
				uid = created.LogicalChannelUID
			}
			before, _ := json.Marshal(cm.GetConfig())
			history := cm.ManagementAuditSnapshot()
			path := cm.configFile
			cm.configFile = t.TempDir()
			input.Name = "second"
			input.BaseURLs = []string{"https://second.example"}
			if _, err := cm.CreateLogicalChannel(input); err == nil {
				t.Fatal("failed create succeeded")
			}
			if existing {
				if _, err := cm.DeleteLogicalChannel(uid); err == nil {
					t.Fatal("failed delete succeeded")
				}
				name := "changed"
				if _, err := cm.UpdateLogicalChannel(UpdateLogicalChannelInput{LogicalChannelUID: uid, Common: &UpdateLogicalChannelCommon{Name: &name}}); err == nil {
					t.Fatal("failed update succeeded")
				}
			}
			cm.configFile = path
			after, _ := json.Marshal(cm.GetConfig())
			if string(before) != string(after) {
				t.Fatal("failed transaction changed existing channels")
			}
			if cm.ManagementAuditSnapshot().NextSequence != history.NextSequence {
				t.Fatal("failure committed audit")
			}
		})
	}
}

func TestManagementAuditContinuousSingleWriterDoesNotReportPendingReload(t *testing.T) {
	cm := auditTestManager(t)
	for i := 0; i < 8; i++ {
		if err := cm.SetRacingEnabledAudited(auditTestContext(strconv.Itoa(i)), i%2 == 0); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagementAuditModelImportTransactionsKeepExplicitActorAndHistory(t *testing.T) {
	cm := newImportManager(t)
	view, _ := cm.GetModelImport("import-channel")
	policyAudit := auditTestContext("model-policy")
	view, err := cm.SetModelImportPolicyAudited(policyAudit, "import-channel", view.Revision, ModelImportPolicy{}, policyAudit.ActorID)
	if err != nil {
		t.Fatal(err)
	}
	if view.State.Audit[len(view.State.Audit)-1].OperationID != policyAudit.OperationID {
		t.Fatal("policy domain audit lost operation")
	}
	if _, err = cm.StageModelImport("import-channel", []ModelImportCandidate{{ID: "background-model"}}, "discovery", view.Revision); err != nil {
		t.Fatal(err)
	}
	background := cm.ManagementAuditSnapshot().Events
	if background[len(background)-1].ActorID != "system" || background[len(background)-1].OperationID == policyAudit.OperationID {
		t.Fatal("background discovery inherited human operation")
	}
	stageAudit := auditTestContext("model-stage")
	review, err := cm.StageModelImportAudited(stageAudit, "import-channel", []ModelImportCandidate{{ID: "safe-model"}}, "operator", view.Revision)
	if err != nil {
		t.Fatal(err)
	}
	approveAudit := auditTestContext("model-approve")
	view, err = cm.ApproveModelImportAudited(approveAudit, "import-channel", review.ID, approveAudit.ActorID, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if view.State.Audit[len(view.State.Audit)-1].OperationID != approveAudit.OperationID {
		t.Fatal("approval domain audit lost operation")
	}
	for _, ctx := range []ManagementAuditContext{policyAudit, stageAudit, approveAudit} {
		if event := auditEventFor(t, cm, ctx.OperationID); event.ActorID != ctx.ActorID {
			t.Fatal("model transaction crossed actors")
		}
	}
	before, _ := json.Marshal(cm.GetConfig())
	sequence := cm.ManagementAuditSnapshot().NextSequence
	path := cm.configFile
	cm.configFile = t.TempDir()
	if _, err = cm.RemoveModelImportAudited(auditTestContext("failed-remove"), "import-channel", view.Revision, "admin", true); err == nil {
		t.Fatal("failed removal succeeded")
	}
	cm.configFile = path
	after, _ := json.Marshal(cm.GetConfig())
	if string(before) != string(after) || cm.ManagementAuditSnapshot().NextSequence != sequence {
		t.Fatal("failed model save published state/audit")
	}
	removeAudit := auditTestContext("model-remove")
	if _, err = cm.RemoveModelImportAudited(removeAudit, "import-channel", view.Revision, removeAudit.ActorID, true); err != nil {
		t.Fatal(err)
	}
	_ = auditEventFor(t, cm, removeAudit.OperationID)
}
