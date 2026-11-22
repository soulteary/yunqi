package messages

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
)

func TestModelEntryDesktopWireMetadata(t *testing.T) {
	data, err := json.Marshal(ModelEntry{ID: "deepseek-v4-pro", Supports1M: true, ContextWindow: 1048576, AnthropicFamilyTier: "sonnet", IsFamilyDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"supports_1m": true, "max_input_tokens": float64(1048576), "anthropic_family_tier": "sonnet", "is_family_default": true, "supports1m": true, "anthropicFamilyTier": "sonnet", "isFamilyDefault": true} {
		if wire[key] != want {
			t.Errorf("%s = %v, want %v", key, wire[key], want)
		}
	}
}

func TestModelEntryReadsDesktopAndLegacyMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, body, tier        string
		supports, familyDefault bool
		window                  int
	}{
		{"desktop", `{"id":"alias","supports_1m":true,"anthropic_family_tier":"opus","is_family_default":true,"max_input_tokens":1000000}`, "opus", true, true, 1000000},
		{"legacy", `{"id":"alias","supports1m":true,"anthropicFamilyTier":"sonnet","isFamilyDefault":true,"context_window":1000000}`, "sonnet", true, true, 1000000},
		{"canonical false wins", `{"supports_1m":false,"supports1m":true,"anthropic_family_tier":"haiku","anthropicFamilyTier":"opus","is_family_default":false,"isFamilyDefault":true}`, "haiku", false, false, 0},
		{"canonical input limit wins", `{"context_window":1048576,"max_input_tokens":200000}`, "", false, false, 200000},
		{"canonical zero wins", `{"context_window":1048576,"max_input_tokens":0}`, "", false, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var model ModelEntry
			if err := json.Unmarshal([]byte(tc.body), &model); err != nil {
				t.Fatal(err)
			}
			if model.AnthropicFamilyTier != tc.tier || model.Supports1M != tc.supports || model.IsFamilyDefault != tc.familyDefault || model.ContextWindow != tc.window {
				t.Fatalf("unexpected metadata: %+v", model)
			}
		})
	}
}

func TestModelsHandlerDesktopDeepSeekDiscovery(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		for _, mapped := range []bool{false, true} {
			name := "live"
			if fallback {
				name = "configured fallback"
			}
			if mapped {
				name += "/explicit tiers"
			} else {
				name += "/no implicit tiers"
			}
			t.Run(name, func(t *testing.T) {
				var unavailable atomic.Bool
				unavailable.Store(fallback)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if unavailable.Load() {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-v4-pro","display_name":"Live Pro"},{"id":"deepseek-flash","display_name":"Live Flash"}]}`))
				}))
				defer server.Close()
				upstream := config.UpstreamConfig{Name: "deepseek", BaseURL: server.URL, APIKeys: []string{"test-upstream"}, ServiceType: "claude", SupportedModels: []string{"deepseek-v4-pro", "deepseek-flash"}}
				if mapped {
					// Decode persisted configuration rather than setting a Go-only value.
					if err := json.Unmarshal([]byte(`{"modelCapabilities":{"deepseek-v4-pro":{"anthropicFamilyTier":"sonnet","contextWindowTokens":1048576},"deepseek-flash":{"anthropicFamilyTier":"haiku","contextWindowTokens":1048576}}}`), &upstream); err != nil {
						t.Fatal(err)
					}
				}
				cm := setupModelsConfigManager(t, config.Config{Upstream: []config.UpstreamConfig{upstream}})
				router := newModelsRouterForAggregate(&config.EnvConfig{ProxyAccessKey: "test-key"}, cm, newModelsTestScheduler(cm))
				req := httptest.NewRequest(http.MethodGet, "/v1/models?limit=1000", nil)
				req.Header.Set("x-api-key", "test-key")
				req.Header.Set("anthropic-version", "2023-06-01")
				recorder := httptest.NewRecorder()
				httpbridge.Handler(router).ServeHTTP(recorder, req)
				if recorder.Code != http.StatusOK {
					t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
				}
				// Use Desktop's wire fields, independently of Yunqi's DTO.
				var response struct {
					Data []struct {
						ID         string                         `json:"id"`
						Tier       string                         `json:"anthropic_family_tier"`
						Supports1M bool                           `json:"supports_1m"`
						Default    bool                           `json:"is_family_default"`
						MaxInput   int                            `json:"max_input_tokens"`
						Endpoints  []config.ModelEndpointMetadata `json:"endpoint_metadata"`
					} `json:"data"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if len(response.Data) != 2 {
					t.Fatalf("models = %+v", response.Data)
				}
				for _, model := range response.Data {
					if model.ID != "deepseek-v4-pro" && model.ID != "deepseek-flash" {
						t.Fatalf("discovery changed the model ID: %q", model.ID)
					}
					for _, endpoint := range model.Endpoints {
						if endpoint.Verified {
							t.Fatalf("tier mapping verified an untested endpoint: %+v", endpoint)
						}
					}
					if !mapped {
						if model.Tier != "" {
							t.Fatalf("unconfigured model classified as Claude: %+v", model)
						}
						continue
					}
					want := "sonnet"
					if model.ID == "deepseek-flash" {
						want = "haiku"
					}
					if model.Tier != want || !model.Supports1M || !model.Default || model.MaxInput != 1048576 {
						t.Fatalf("Desktop cannot use model metadata: %+v", model)
					}
				}
				if !fallback {
					unavailable.Store(true)
					cached := httptest.NewRecorder()
					httpbridge.Handler(router).ServeHTTP(cached, req.Clone(req.Context()))
					if cached.Code != http.StatusOK || !bytes.Equal(cached.Body.Bytes(), recorder.Body.Bytes()) {
						t.Fatalf("cached discovery changed metadata: %d %s", cached.Code, cached.Body.String())
					}
				}
			})
		}
	}
}

func TestNormalizeModelEntryDesktopTierPrecedence(t *testing.T) {
	for _, tc := range []struct{ name, configured, provider, want string }{
		{"explicit override", " Sonnet ", "opus", "sonnet"},
		{"provider metadata", "", " HAIKU ", "haiku"},
		{"invalid configured tier", "deepseek", "", ""},
		{"invalid provider tier", "", "invalid", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &config.UpstreamConfig{ModelCapabilities: map[string]config.UpstreamModelCapability{"deepseek-flash": {AnthropicFamilyTier: tc.configured}}}
			model := normalizeModelEntry(ModelEntry{ID: "deepseek-flash", AnthropicFamilyTier: tc.provider}, upstream, nil)
			if model.AnthropicFamilyTier != tc.want {
				t.Fatalf("tier = %q, want %q", model.AnthropicFamilyTier, tc.want)
			}
		})
	}
	response := buildModelsResponse([]ModelEntry{{ID: "a", AnthropicFamilyTier: "sonnet"}, {ID: "b", AnthropicFamilyTier: "sonnet", IsFamilyDefault: true}})
	if !response.Data[0].IsFamilyDefault || response.Data[1].IsFamilyDefault {
		t.Fatalf("multiple defaults in one tier: %+v", response.Data)
	}
}

func TestParseModelsResponseDesktopInputWindow(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		window     int
		supports1m bool
	}{
		{"provider limit below registry", `{"data":[{"id":"claude-sonnet-4-6","context_window":1048576,"max_input_tokens":200000}]}`, 200000, false},
		{"provider window without a registry entry", `{"data":[{"id":"custom-window-model","max_input_tokens":1048576}]}`, 1048576, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models := parseModelsResponseForKind([]byte(tc.body), &config.UpstreamConfig{ServiceType: "claude"}, nil, scheduler.ChannelKindMessages)
			if len(models) != 1 || models[0].ContextWindow != tc.window || models[0].Supports1M != tc.supports1m {
				t.Fatalf("unexpected provider window metadata: %+v", models)
			}
		})
	}
}

func TestNormalizeModelEntryDedicatedDesktopTiers(t *testing.T) {
	upstream := &config.UpstreamConfig{ClaudeDesktopModelTiers: map[string]string{"deepseek-v4-pro": "sonnet", "deepseek-flash": "haiku"}}
	for _, id := range []string{"deepseek-v4-pro", "deepseek-flash"} {
		baseline := normalizeModelEntry(ModelEntry{ID: id}, nil, nil)
		model := normalizeModelEntry(ModelEntry{ID: id}, upstream, nil)
		if model.AnthropicFamilyTier != upstream.ClaudeDesktopModelTiers[id] || model.ContextWindow != baseline.ContextWindow || model.MaxOutputTokens != baseline.MaxOutputTokens {
			t.Fatalf("mapping changed capacity: %+v", model)
		}
	}
	upstream.ModelCapabilities = map[string]config.UpstreamModelCapability{"deepseek-v4-pro": {AnthropicFamilyTier: "opus"}}
	upstream.ClaudeDesktopModelTiers["deepseek-v4-pro"] = ""
	cleared := normalizeModelEntry(ModelEntry{ID: "deepseek-v4-pro", AnthropicFamilyTier: "haiku", IsFamilyDefault: true}, upstream, nil)
	if cleared.AnthropicFamilyTier != "" || cleared.IsFamilyDefault {
		t.Fatalf("clear retained inherited tier: %+v", cleared)
	}
}

func TestDesktopTierChangeInvalidatesDiscoveryCache(t *testing.T) {
	var unavailable atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-v4-pro","display_name":"Cached provider name"}]}`))
	}))
	defer server.Close()
	cm := setupModelsConfigManager(t, config.Config{Upstream: []config.UpstreamConfig{{Name: "deepseek", BaseURL: server.URL, APIKeys: []string{"test"}, ServiceType: "claude", SupportedModels: []string{"deepseek-v4-pro"}, ClaudeDesktopModelTiers: map[string]string{"deepseek-v4-pro": "sonnet"}}}})
	router := newModelsRouterForAggregate(&config.EnvConfig{ProxyAccessKey: "test-key"}, cm, newModelsTestScheduler(cm))
	read := func() ModelEntry {
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		req.Header.Set("x-api-key", "test-key")
		rec := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("discovery failed: %d %s", rec.Code, rec.Body.String())
		}
		var response ModelsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Data) != 1 {
			t.Fatalf("unexpected models: %+v", response.Data)
		}
		return response.Data[0]
	}
	if got := read().AnthropicFamilyTier; got != "sonnet" {
		t.Fatalf("initial tier = %q", got)
	}
	unavailable.Store(true)
	if _, err := cm.UpdateUpstream(0, config.UpstreamUpdate{ClaudeDesktopModelTiers: map[string]string{"deepseek-v4-pro": "haiku"}}); err != nil {
		t.Fatal(err)
	}
	if got := read().AnthropicFamilyTier; got != "haiku" {
		t.Fatalf("cache retained old tier: %q", got)
	}
	if _, err := cm.UpdateUpstream(0, config.UpstreamUpdate{ClaudeDesktopModelTiers: map[string]string{"deepseek-v4-pro": ""}}); err != nil {
		t.Fatal(err)
	}
	if got := read().AnthropicFamilyTier; got != "" {
		t.Fatalf("cache retained cleared tier: %q", got)
	}
}

func TestDesktopTierOverrideSurvivesProtocolMerge(t *testing.T) {
	for _, tier := range []string{"sonnet", ""} {
		t.Run("tier="+tier, func(t *testing.T) {
			explicit := normalizeModelEntry(ModelEntry{ID: "deepseek-v4-pro"}, &config.UpstreamConfig{ClaudeDesktopModelTiers: map[string]string{"deepseek-v4-pro": tier}}, nil)
			inherited := normalizeModelEntry(ModelEntry{ID: "deepseek-v4-pro", AnthropicFamilyTier: "opus", IsFamilyDefault: true}, nil, nil)
			for _, entries := range [][]ModelEntry{mergeModels([]ModelEntry{explicit}, []ModelEntry{inherited}), mergeModels([]ModelEntry{inherited}, []ModelEntry{explicit})} {
				result := buildModelsResponse(entries)
				if len(result.Data) != 1 || result.Data[0].AnthropicFamilyTier != tier {
					t.Fatalf("protocol merge replaced explicit setting: %+v", result.Data)
				}
				if tier == "" && result.Data[0].IsFamilyDefault {
					t.Fatal("cleared tier retained a default flag")
				}
			}
		})
	}
}

func TestDesktopTierOverridesInheritedPatterns(t *testing.T) {
	up := &config.UpstreamConfig{ModelCapabilities: map[string]config.UpstreamModelCapability{"deepseek-*": {AnthropicFamilyTier: "sonnet", ContextWindowTokens: 200000}}}
	for _, id := range []string{"deepseek-v4-pro", "deepseek-flash"} {
		if got := normalizeModelEntry(ModelEntry{ID: id}, up, nil).AnthropicFamilyTier; got != "sonnet" {
			t.Fatalf("inherited tier for %s = %q", id, got)
		}
	}
	up.ClaudeDesktopModelTiers = map[string]string{"deepseek-v4-pro": "haiku", "deepseek-flash": ""}
	for id, want := range up.ClaudeDesktopModelTiers {
		model := normalizeModelEntry(ModelEntry{ID: id}, up, nil)
		if model.AnthropicFamilyTier != want || model.ContextWindow != 200000 {
			t.Fatalf("override for %s ineffective: %+v", id, model)
		}
	}
}
