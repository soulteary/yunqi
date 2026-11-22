package autopilot

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
)

func TestModelImportDiscoveryOnlyStagesReviewedEvidence(t *testing.T) {
	cm := setupTestConfigManagerForDiscovery(t, "governed", []string{"legacy"})
	view, err := cm.GetModelImport("governed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cm.SetModelImportPolicy("governed", view.Revision, config.ModelImportPolicy{Enabled: true, Include: []string{`^provider/a$`}, StripPrefix: "provider/"}, "admin"); err != nil {
		t.Fatal(err)
	}
	channel := cm.GetConfig().Upstream[0]
	endpoints := []EndpointDiscoveryResult{{ProtocolOk: true, Models: []string{"provider/a", "provider/b"}, ProtocolModels: map[string][]string{"chat": {"provider/a"}, "responses": {"provider/a"}}, ProtocolDiscoverySource: map[string]string{"chat": "protocol_model_probe", "responses": "models_api"}}}
	original := append([]string(nil), endpoints[0].Models...)
	runner := NewAutoDiscoveryRunner(nil, nil)
	if err = runner.maybeAutoWriteChannelConfig("governed", &channel, endpoints, cm); err != nil {
		t.Fatal(err)
	}
	staged, _ := cm.GetModelImport("governed")
	if staged.State.Pending == nil || len(staged.State.Pending.Models) != 1 || len(staged.State.Approved) != 0 {
		t.Fatalf("discovery activated or lost review %+v", staged)
	}
	item := staged.State.Pending.Models[0]
	if item.ID != "a" || item.UpstreamID != "provider/a" {
		t.Fatalf("provider names were rewritten in raw inventory %+v", item)
	}
	if !reflect.DeepEqual(original, endpoints[0].Models) || !reflect.DeepEqual(cm.GetConfig().Upstream[0].SupportedModels, []string{"legacy"}) {
		t.Fatal("discovery replaced live/raw inventories")
	}
	proof := map[string]config.ModelEndpointMetadata{}
	for _, ep := range item.Endpoints {
		proof[ep.Type] = ep
	}
	if !proof["chat"].Verified || proof["chat"].Source != "probe" || proof["responses"].Verified || proof["responses"].Source != "provider_declared" {
		t.Fatalf("declaration became probe evidence %+v", proof)
	}
	// A provider URL/key/policy edit during discovery invalidates its original
	// snapshot instead of binding old evidence to a new supplier configuration.
	description := "source changed"
	if _, err = cm.UpdateUpstream(0, config.UpstreamUpdate{Description: &description}); err != nil {
		t.Fatal(err)
	}
	if err = stageDiscoveredModels("governed", &channel, endpoints, cm); !errors.Is(err, config.ErrModelImportConflict) {
		t.Fatalf("stale discovery evidence accepted: %v", err)
	}
}
func TestModelImportNewProtocolDoesNotInheritAnotherRoutesApproval(t *testing.T) {
	source := &config.UpstreamConfig{ServiceType: "openai", BaseURL: "https://example.invalid", ModelImport: &config.ModelImportState{Policy: config.ModelImportPolicy{Enabled: true}, Approved: []config.ImportedModel{{ID: "public", UpstreamID: "raw"}}, Pending: &config.ModelImportReview{ID: "old-review"}}}
	created := discoveredProtocolUpstream(source, "responses", "test", true)
	if created.ModelImport == nil || !created.ModelImport.Policy.Enabled || len(created.ModelImport.Approved) != 0 || created.ModelImport.Pending != nil {
		t.Fatalf("new route inherited approval/evidence %+v", created.ModelImport)
	}
	if len(source.ModelImport.Approved) != 1 {
		t.Fatal("source route was mutated")
	}
}

func TestModelImportQuoteUsesReviewedProviderPrice(t *testing.T) {
	cfg := baseTestConfig()
	rawPrice, aliasPrice := 2.0, 0.01
	for i := range cfg.Upstream {
		cfg.Upstream[i].SupportedModels = []string{"provider/raw"}
		cfg.Upstream[i].ModelImport = &config.ModelImportState{Policy: config.ModelImportPolicy{Enabled: true}, Approved: []config.ImportedModel{{ID: "public", UpstreamID: "provider/raw"}}}
		cfg.Upstream[i].ModelCapabilities = map[string]config.UpstreamModelCapability{
			"provider/raw": {ContextWindowTokens: 2000000, MaxOutputTokens: 1000000, Pricing: &config.ModelPricing{InputCacheMissPrice: &rawPrice, OutputPrice: &rawPrice}},
			"public":       {ContextWindowTokens: 2000000, MaxOutputTokens: 1000000, Pricing: &config.ModelPricing{InputCacheMissPrice: &aliasPrice, OutputPrice: &aliasPrice}},
		}
	}
	sch, cm, cleanup := createTestScheduler(t, cfg)
	defer cleanup()
	app := httpbridge.NewApp()
	app.Post("/v1/quote", CallerQuoteHandler(cm, sch))
	request := httptest.NewRequest("POST", "/v1/quote", strings.NewReader(`{"channelKind":"messages","model":"public","inputTokens":1000000,"outputTokens":500000}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	httpbridge.Handler(app).ServeHTTP(response, request)
	var result struct {
		ResolvedModel string  `json:"resolvedModel"`
		EstimatedCost float64 `json:"estimatedCost"`
		Complete      bool    `json:"pricingComplete"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || result.ResolvedModel != "provider/raw" || result.EstimatedCost != 3 || !result.Complete {
		t.Fatalf("quote borrowed alias price: %d %s", response.Code, response.Body.String())
	}
}

func TestModelImportDraftDiscoveryDoesNotWriteWhitelistOrStaleItself(t *testing.T) {
	cm := setupTestConfigManagerForDiscovery(t, "governed", nil)
	view, err := cm.GetModelImport("governed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cm.SetModelImportPolicy("governed", view.Revision, config.ModelImportPolicy{Enabled: false, Include: []string{`^provider/a$`}, StripPrefix: "provider/"}, "admin"); err != nil {
		t.Fatal(err)
	}
	channel := cm.GetConfig().Upstream[0]
	endpoints := []EndpointDiscoveryResult{{ProtocolOk: true, Models: []string{"provider/a", "provider/b"}, ProtocolModels: map[string][]string{"chat": {"provider/a"}, "responses": {"provider/a"}}, ProtocolDiscoverySource: map[string]string{"chat": "protocol_model_probe", "responses": "models_api"}}}
	original := append([]string(nil), endpoints[0].Models...)
	runner := NewAutoDiscoveryRunner(nil, nil)
	if err = runner.maybeAutoWriteChannelConfig("governed", &channel, endpoints, cm); err != nil {
		t.Fatal(err)
	}
	staged, _ := cm.GetModelImport("governed")
	if staged.State.Pending == nil || len(staged.State.Pending.Models) != 1 || len(staged.State.Approved) != 0 {
		t.Fatalf("discovery activated or lost review %+v", staged)
	}
	item := staged.State.Pending.Models[0]
	if item.ID != "a" || item.UpstreamID != "provider/a" {
		t.Fatalf("provider names were rewritten in raw inventory %+v", item)
	}
	if !reflect.DeepEqual(original, endpoints[0].Models) || len(cm.GetConfig().Upstream[0].SupportedModels) != 0 {
		t.Fatal("discovery replaced live/raw inventories")
	}
	proof := map[string]config.ModelEndpointMetadata{}
	for _, ep := range item.Endpoints {
		proof[ep.Type] = ep
	}
	if !proof["chat"].Verified || proof["chat"].Source != "probe" || proof["responses"].Verified || proof["responses"].Source != "provider_declared" {
		t.Fatalf("declaration became probe evidence %+v", proof)
	}
	if _, err = cm.ApproveModelImport("governed", staged.State.Pending.ID, "admin", false, false); err != nil {
		t.Fatalf("first discovery invalidated its own draft: %v", err)
	}
}

type modelImportRoundTrip func(*http.Request) (*http.Response, error)

func (f modelImportRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestModelImportLegacyDiscoveryReportsStaleStage(t *testing.T) {
	cm := setupTestConfigManagerForDiscovery(t, "governed", []string{"legacy"})
	view, _ := cm.GetModelImport("governed")
	if _, err := cm.SetModelImportPolicy("governed", view.Revision, config.ModelImportPolicy{Enabled: false}, "admin"); err != nil {
		t.Fatal(err)
	}
	channel := cm.GetConfig().Upstream[0]
	runner := NewAutoDiscoveryRunner(nil, nil)
	var once sync.Once
	runner.client = &http.Client{Transport: modelImportRoundTrip(func(r *http.Request) (*http.Response, error) {
		once.Do(func() {
			description := "concurrent edit"
			if _, err := cm.UpdateUpstream(0, config.UpstreamUpdate{Description: &description}); err != nil {
				t.Error(err)
			}
		})
		raw := `{"object":"list","data":[{"id":"raw","object":"model"}],"id":"chat","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	task := &DiscoveryTask{ChannelUID: "governed"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runner.runDiscoveryLegacy(ctx, task, &channel, cm)
	if task.Status != DiscoveryStatusFailed || !strings.Contains(task.Error, "review") {
		t.Fatalf("legacy discovery hid staging failure: status=%s error=%s", task.Status, task.Error)
	}
	state, _ := cm.GetModelImport("governed")
	if state.State.Pending != nil {
		t.Fatal("stale legacy evidence staged")
	}
}

func TestModelImportPolicyAddedDuringLegacyDiscoveryBlocksOldSnapshotWrites(t *testing.T) {
	cm := setupTestConfigManagerForDiscovery(t, "governed", nil)
	legacy := false
	if _, err := cm.UpdateUpstream(0, config.UpstreamUpdate{AutoManaged: &legacy}); err != nil {
		t.Fatal(err)
	}
	source := cm.GetConfig().Upstream[0]
	view, _ := cm.GetModelImport(source.ChannelUID)
	if _, err := cm.SetModelImportPolicy(source.ChannelUID, view.Revision, config.ModelImportPolicy{Enabled: false}, "admin"); err != nil {
		t.Fatal(err)
	}
	runner := NewAutoDiscoveryRunner(nil, nil)
	endpoints := []EndpointDiscoveryResult{{ProtocolOk: true, Models: []string{"discovered"}, usedClientFingerprint: true}}
	writeErr := runner.maybeAutoWriteChannelConfig(source.ChannelUID, &source, endpoints, cm)
	runner.maybeLearnClientFingerprint(source.ChannelUID, &source, endpoints, cm)
	current := cm.GetConfig().Upstream[0]
	if len(current.SupportedModels) > 0 || current.LearnedClientFingerprint {
		t.Fatalf("old discovery changed new draft: models=%v fingerprint=%v", current.SupportedModels, current.LearnedClientFingerprint)
	}
	if !errors.Is(writeErr, config.ErrModelImportConflict) {
		t.Fatalf("stale legacy source was not rejected: %v", writeErr)
	}
}
