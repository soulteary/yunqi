package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func newImportManager(t *testing.T) *ConfigManager {
	t.Helper()
	return &ConfigManager{configFile: filepath.Join(t.TempDir(), "config.json"), config: Config{ChatUpstream: []UpstreamConfig{{ChannelUID: "import-channel", ServiceType: "openai", BaseURL: "https://import.invalid", APIKeys: []string{"fake-provider-key"}, Status: "active", SupportedModels: []string{"legacy-model"}}}}}
}
func enableImport(t *testing.T, cm *ConfigManager, policy ModelImportPolicy) ModelImportView {
	t.Helper()
	view, err := cm.GetModelImport("import-channel")
	if err != nil {
		t.Fatal(err)
	}
	view, err = cm.SetModelImportPolicy("import-channel", view.Revision, policy, "test-admin")
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func TestModelImportFiltersOriginalIDsAndPreservesProviderTargets(t *testing.T) {
	policy := ModelImportPolicy{Enabled: true, Include: []string{`^provider/gpt-`}, Exclude: []string{`-blocked$`}, StripPrefix: "provider/", StripSuffix: "-latest", Rules: []ModelImportRule{{Pattern: `^gpt-(.*)$`, Replacement: `public-$1`}}}
	models, err := BuildModelImport(policy, []ModelImportCandidate{{ID: "provider/gpt-fast-latest", Endpoints: []ModelEndpointMetadata{{Type: "openai", Source: "probe", Verified: true}}}, {ID: "provider/gpt-fast-latest"}, {ID: "provider/gpt-blocked"}, {ID: "provider/other"}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "public-fast" || models[0].UpstreamID != "provider/gpt-fast-latest" {
		t.Fatalf("incorrect normalized catalog %+v", models)
	}
	if len(models[0].Endpoints) != 1 || models[0].Endpoints[0].Verified || models[0].Endpoints[0].Source != "operator" {
		t.Fatalf("manual input forged protocol evidence %+v", models[0].Endpoints)
	}
	for _, tc := range []struct {
		policy ModelImportPolicy
		models []ModelImportCandidate
	}{
		{ModelImportPolicy{StripPrefix: "provider/"}, []ModelImportCandidate{{ID: "provider/gpt"}, {ID: "gpt"}}},
		{ModelImportPolicy{Rules: []ModelImportRule{{Pattern: `.*`, Replacement: "one"}}}, []ModelImportCandidate{{ID: "a"}, {ID: "b"}}},
		{ModelImportPolicy{Include: []string{`(?=unsupported)`}}, []ModelImportCandidate{{ID: "a"}}},
		{ModelImportPolicy{Rules: []ModelImportRule{{Pattern: `.`, Replacement: strings.Repeat("x", 256)}, {Pattern: `.`, Replacement: strings.Repeat("x", 256)}}}, []ModelImportCandidate{{ID: "input"}}},
		{ModelImportPolicy{}, []ModelImportCandidate{{ID: "../admin"}}},
	} {
		if _, err := BuildModelImport(tc.policy, tc.models, "operator"); !errors.Is(err, ErrInvalidModelImport) {
			t.Fatalf("unsafe import accepted: %+v %v", tc.policy, err)
		}
	}
}
func TestModelImportReviewActivationStalenessAndSnapshots(t *testing.T) {
	cm := newImportManager(t)
	policy := ModelImportPolicy{Enabled: true, StripPrefix: "provider/", Include: []string{`^provider/`}}
	enableImport(t, cm, policy)
	policy.Include[0] = "mutated"
	if ok, _ := cm.GetConfig().ChatUpstream[0].ExplainModelSupport("gpt-test"); ok {
		t.Fatal("unreviewed policy used empty=all fallback")
	}
	review, err := cm.StageModelImport("import-channel", []ModelImportCandidate{{ID: "provider/gpt-test"}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	id := review.ID
	review.Models[0].UpstreamID = "forged"
	pending, _ := cm.GetModelImport("import-channel")
	if pending.State.Pending.Models[0].UpstreamID != "provider/gpt-test" || pending.State.Policy.Include[0] != `^provider/` {
		t.Fatal("caller mutated server-owned state")
	}
	if _, err = cm.ApproveModelImport("import-channel", "wrong-id", "test-admin", false, false); !errors.Is(err, ErrModelImportConflict) {
		t.Fatalf("wrong review accepted: %v", err)
	}
	view, err := cm.ApproveModelImport("import-channel", id, "test-admin", false, false)
	if err != nil {
		t.Fatal(err)
	}
	channel := cm.GetConfig().ChatUpstream[0]
	if actual, err := channel.CheckImportedModel("gpt-test"); err != nil || actual != "provider/gpt-test" {
		t.Fatalf("provider ID lost: %q %v", actual, err)
	}
	if ok, _ := channel.ExplainModelSupport("other"); ok {
		t.Fatal("unapproved model admitted")
	}
	if !reflect.DeepEqual(channel.SupportedModels, []string{"provider/gpt-test"}) || view.State.Pending != nil {
		t.Fatalf("review not activated %+v", view)
	}
	// Public aliases cannot borrow a cheaper canonical model's price or window.
	price := 0.01
	cap := ResolveUpstreamCapability("gpt-test", &channel, map[string]UpstreamModelCapability{"gpt-test": {ContextWindowTokens: 1000000, Pricing: &ModelPricing{OutputPrice: &price}}})
	if cap.Known || cap.ActualModel != "provider/gpt-test" {
		t.Fatalf("alias inferred unsupported price/capability equivalence %+v", cap)
	}
	next, err := cm.StageModelImport("import-channel", []ModelImportCandidate{{ID: "provider/gpt-next"}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	description := "changed after preview"
	if _, err = cm.UpdateChatUpstream(0, UpstreamUpdate{Description: &description}); err != nil {
		t.Fatal(err)
	}
	if _, err = cm.ApproveModelImport("import-channel", next.ID, "test-admin", false, true); !errors.Is(err, ErrModelImportConflict) {
		t.Fatalf("stale channel snapshot accepted: %v", err)
	}
	fresh, err := cm.StageModelImport("import-channel", nil, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cm.ApproveModelImport("import-channel", fresh.ID, "test-admin", false, true); !errors.Is(err, ErrInvalidModelImport) {
		t.Fatalf("empty catalog needs acknowledgement: %v", err)
	}
	if _, err = cm.ApproveModelImport("import-channel", fresh.ID, "test-admin", true, false); !errors.Is(err, ErrInvalidModelImport) {
		t.Fatalf("removal needs acknowledgement: %v", err)
	}
	if _, err = cm.ApproveModelImport("import-channel", fresh.ID, "test-admin", true, true); err != nil {
		t.Fatal(err)
	}
	if ok, _ := cm.GetConfig().ChatUpstream[0].ExplainModelSupport("anything"); ok {
		t.Fatal("approved empty catalog became unrestricted")
	}
	loaded, err := NewConfigManager(cm.configFile, "")
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if ok, _ := loaded.GetConfig().ChatUpstream[0].ExplainModelSupport("anything"); ok {
		t.Fatal("restart lost empty catalog gate")
	}
}
func TestModelImportFailedPersistenceDoesNotPublishPolicyOrApproval(t *testing.T) {
	cm := newImportManager(t)
	enableImport(t, cm, ModelImportPolicy{Enabled: true})
	review, err := cm.StageModelImport("import-channel", []ModelImportCandidate{{ID: "allowed"}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	cm.configFile = filepath.Join(t.TempDir(), "missing", "config.json")
	before := cm.config.deepCopy()
	if _, err = cm.ApproveModelImport("import-channel", review.ID, "test-admin", false, false); err == nil {
		t.Fatal("expected persistence failure")
	}
	if !reflect.DeepEqual(cm.config, before) {
		t.Fatal("failed approval changed runtime state")
	}
	view, _ := cm.GetModelImport("import-channel")
	if _, err = cm.SetModelImportPolicy("import-channel", view.Revision, ModelImportPolicy{Enabled: false}, "test-admin"); err == nil {
		t.Fatal("expected policy persistence failure")
	}
	if !reflect.DeepEqual(cm.config, before) {
		t.Fatal("failed policy save changed runtime state")
	}
	if _, err = os.Stat(cm.configFile); !os.IsNotExist(err) {
		t.Fatalf("failed commit created file: %v", err)
	}
}
func TestModelImportGenericCreationAndEditingCannotForgeReview(t *testing.T) {
	cm := newImportManager(t)
	malicious := UpstreamConfig{ChannelUID: "forged-channel", ServiceType: "openai", BaseURL: "https://forged.invalid", APIKeys: []string{"fake"}, ModelImport: &ModelImportState{Policy: ModelImportPolicy{Enabled: true}, Approved: []ImportedModel{{ID: "fake-approved", UpstreamID: "fake-approved", Endpoints: []ModelEndpointMetadata{{Type: "chat", Source: "probe", Verified: true}}}}}}
	if err := cm.AddChatUpstream(malicious); !errors.Is(err, ErrInvalidModelImport) {
		t.Fatalf("generic create accepted approval evidence: %v", err)
	}
	enableImport(t, cm, ModelImportPolicy{Enabled: true})
	review, err := cm.StageModelImport("import-channel", []ModelImportCandidate{{ID: "original"}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cm.ApproveModelImport("import-channel", review.ID, "test-admin", false, false); err != nil {
		t.Fatal(err)
	}
	before, _ := cm.GetModelImport("import-channel")
	raw := `{"remark":"edit","modelImport":{"policy":{"enabled":false},"approved":[{"id":"forged","upstreamId":"forged"}],"pending":{"id":"forged"}}}`
	var update UpstreamUpdate
	if err = json.Unmarshal([]byte(raw), &update); err != nil {
		t.Fatal(err)
	}
	if _, err = cm.UpdateChatUpstream(0, update); err != nil {
		t.Fatalf("edit carrying opaque state failed: %v", err)
	}
	after, _ := cm.GetModelImport("import-channel")
	if !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("generic update replaced server-owned catalog")
	}
	if cm.GetConfig().ChatUpstream[0].Remark != "edit" {
		t.Fatal("edit was dropped")
	}
}

func TestModelImportDraftDoesNotInterruptCallsBeforeAtomicActivation(t *testing.T) {
	cm := newImportManager(t)
	view := enableImport(t, cm, ModelImportPolicy{Enabled: false, StripPrefix: "provider/", Case: "lower"})
	if ok, _ := cm.GetConfig().ChatUpstream[0].ExplainModelSupport("legacy-model"); !ok {
		t.Fatal("draft policy interrupted existing model")
	}
	review, err := cm.StageModelImport("import-channel", []ModelImportCandidate{{ID: "provider/NEW"}}, "operator", view.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := cm.GetConfig().ChatUpstream[0].ExplainModelSupport("legacy-model"); !ok {
		t.Fatal("preview changed live admission")
	}
	if _, err = cm.ApproveModelImport("import-channel", review.ID, "admin", false, false); err != nil {
		t.Fatal(err)
	}
	channel := cm.GetConfig().ChatUpstream[0]
	if actual, err := channel.CheckImportedModel("new"); err != nil || actual != "provider/NEW" {
		t.Fatalf("case normalization lost original ID: %q %v", actual, err)
	}
	if !channel.ModelImportEnabled() {
		t.Fatal("approval did not atomically enable catalog")
	}
	if ok, _ := channel.ExplainModelSupport("legacy-model"); ok {
		t.Fatal("activation retained unreviewed legacy model")
	}
	if _, err = cm.StageModelImport("import-channel", []ModelImportCandidate{{ID: "provider/OTHER"}}, "operator", view.Revision); !errors.Is(err, ErrModelImportConflict) {
		t.Fatalf("preview accepted stale revision: %v", err)
	}
}

func TestModelImportExplicitRemovalRestoresLegacyWithAcknowledgement(t *testing.T) {
	cm := newImportManager(t)
	enableImport(t, cm, ModelImportPolicy{Enabled: false})
	cm.config.ChatUpstream[0].SupportedModels = nil
	view, _ := cm.GetModelImport("import-channel")
	if !view.UnrestrictedOnRemove {
		t.Fatal("unsafe widening was not exposed")
	}
	if _, err := cm.RemoveModelImport("import-channel", view.Revision, "admin", false); !errors.Is(err, ErrInvalidModelImport) {
		t.Fatalf("unacknowledged unrestricted reset: %v", err)
	}
	if _, err := cm.RemoveModelImport("import-channel", "stale", "admin", true); !errors.Is(err, ErrModelImportConflict) {
		t.Fatalf("stale reset: %v", err)
	}
	if _, err := cm.RemoveModelImport("import-channel", view.Revision, "admin", true); err != nil {
		t.Fatal(err)
	}
	if cm.GetConfig().ChatUpstream[0].ModelImport != nil {
		t.Fatal("governance not removed")
	}
	enableImport(t, cm, ModelImportPolicy{Enabled: true})
	cm.config.ChatUpstream[0].SupportedModels = []string{"approved-raw"}
	view, _ = cm.GetModelImport("import-channel")
	if _, err := cm.RemoveModelImport("import-channel", view.Revision, "admin", false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cm.GetConfig().ChatUpstream[0].SupportedModels, []string{"approved-raw"}) {
		t.Fatal("reset lost existing whitelist")
	}
}
