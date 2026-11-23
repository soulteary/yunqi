package messages

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestClientMappingsDiscoveryAndRealMessagesRouting(t *testing.T) {
	var received []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/models") {
			io.WriteString(w, `{"data":[{"id":"deepseek-v4-pro","max_input_tokens":200000}]}`)
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		received = append(received, body.Model)
		io.WriteString(w, `{"id":"msg_test","type":"message","role":"assistant","model":"deepseek-v4-pro","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()
	slots := []string{"haiku", "sonnet", "opus", "fable", "mythos"}
	mapping := config.ClientModelMappings{"claude-desktop": {}}
	for _, slot := range slots {
		mapping["claude-desktop"][slot] = "deepseek-v4-pro"
	}
	cm := setupModelsConfigManager(t, config.Config{Upstream: []config.UpstreamConfig{{Name: "deepseek", BaseURL: server.URL, APIKeys: []string{"upstream-test"}, ServiceType: "claude", SupportedModels: []string{"deepseek-v4-pro"}}}, ClientModelMappings: mapping})
	env := &config.EnvConfig{ProxyAccessKey: "test-key", MaxRequestBodySize: 1 << 20}
	sch := newModelsTestScheduler(cm)
	router := newModelsRouterForAggregate(env, cm, sch)
	router.Post("/v1/messages", Handler(env, cm, sch))
	router.Post("/v1/messages/count_tokens", CountTokensHandler(env, cm, sch))
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("x-api-key", "test-key")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(rec, req)
		return rec
	}
	rec := request("GET", "/v1/models?limit=1000", "")
	if rec.Code != 200 {
		t.Fatalf("discovery: %d %s", rec.Code, rec.Body.String())
	}
	var result ModelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 6 {
		t.Fatalf("expected actual model plus five aliases: %+v", result.Data)
	}
	for _, model := range result.Data {
		if model.ContextWindow != 200000 || model.Supports1M {
			t.Fatalf("mapping inflated actual context: %+v", model)
		}
	}
	for _, slot := range slots {
		alias := config.ClientMappingAlias("claude-desktop", slot)
		detail := request("GET", "/v1/models/"+alias, "")
		if detail.Code != 200 || !strings.Contains(detail.Body.String(), `"anthropic_family_tier":"`+slot+`"`) {
			t.Fatalf("detail %s: %d %s", slot, detail.Code, detail.Body.String())
		}
		rec = request("POST", "/v1/messages", `{"model":"`+alias+`","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`)
		if rec.Code != 200 {
			t.Fatalf("routing %s: %d %s", slot, rec.Code, rec.Body.String())
		}
	}
	if len(received) != 5 {
		t.Fatalf("received %v", received)
	}
	for _, model := range received {
		if model != "deepseek-v4-pro" {
			t.Fatalf("alias leaked upstream: %s", model)
		}
	}

	// Existing restricted entry keys authorize the actual target, not its alias.
	for _, tc := range []struct {
		models []string
		status int
	}{
		{[]string{"deepseek-v4-pro"}, 200},
		{[]string{"not-the-target"}, 403},
		{[]string{"yunqi-claude-desktop-sonnet"}, 403},
	} {
		env.ManagedProxyKeyLookup = func(string) (config.EntryKey, bool) {
			return config.EntryKey{ID: "restricted", PrincipalID: "principal", Models: tc.models}, true
		}
		rec = request("POST", "/v1/messages", `{"model":"yunqi-claude-desktop-sonnet","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`)
		if rec.Code != tc.status {
			t.Fatalf("target ACL %v: %d %s", tc.models, rec.Code, rec.Body.String())
		}
	}
	env.ManagedProxyKeyLookup = nil
	// Cached discovery must immediately reflect a clear, without stale aliases.
	if err := cm.SetClientModelMappingsAudited(config.ManagementAuditContext{}, config.ClientModelMappings{"claude-desktop": {}}); err != nil {
		t.Fatal(err)
	}
	rec = request("GET", "/v1/models", "")
	if strings.Contains(rec.Body.String(), "yunqi-claude-desktop-") {
		t.Fatal("cleared alias still advertised")
	}
	if rec = request("POST", "/v1/messages", `{"model":"yunqi-claude-desktop-sonnet","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`); rec.Code != 400 {
		t.Fatalf("cleared alias: %d %s", rec.Code, rec.Body.String())
	}
}

func TestClientProfilesKeepDesktopDiscoverySeparate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"id":"target","max_input_tokens":200000}]}`)
	}))
	defer server.Close()
	up := config.UpstreamConfig{Status: "active", Name: "test", BaseURL: server.URL, APIKeys: []string{"test"}, ServiceType: "claude", SupportedModels: []string{"target"}, ClaudeDesktopModelTiers: map[string]string{"target": "sonnet"}}
	cm := setupModelsConfigManager(t, config.Config{Upstream: []config.UpstreamConfig{up}, ResponsesUpstream: []config.UpstreamConfig{up}, ClientModelMappings: config.ClientModelMappings{"claude-code": {"haiku": "target"}, "codex": {"default": "target"}}})
	router := newModelsRouterForAggregate(&config.EnvConfig{ProxyAccessKey: "test-key"}, cm, newModelsTestScheduler(cm))
	for _, client := range []string{"claude-code", "codex"} {
		slot := "haiku"
		if client == "codex" {
			slot = "default"
		}
		alias := config.ClientMappingAlias(client, slot)
		for _, prefix := range []string{"", "/private"} {
			req := httptest.NewRequest("GET", prefix+"/v1/models/"+alias, nil)
			req.Header.Set("x-api-key", "test-key")
			rec := httptest.NewRecorder()
			httpbridge.Handler(router).ServeHTTP(rec, req)
			expected := 200
			if prefix != "" {
				expected = 404
			}
			if rec.Code != expected {
				t.Fatalf("%s: %d %s", req.URL.Path, rec.Code, rec.Body.String())
			}
		}
	}
	router.Get("/test-models", func(c fiber.Ctx) error {
		result := withClientModelAliases(c, cm, buildModelsResponse([]ModelEntry{{ID: "target", AnthropicFamilyTier: "sonnet", ContextWindow: 200000}}))
		httpbridge.JSON(c, 200, result)
		return nil
	})
	rec := httptest.NewRecorder()
	httpbridge.Handler(router).ServeHTTP(rec, httptest.NewRequest("GET", "/test-models", nil))
	var result ModelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 1 || result.Data[0].AnthropicFamilyTier != "sonnet" {
		t.Fatalf("another client changed Desktop discovery: %+v", result.Data)
	}
}

func TestClientMappingDiscoveryIncludesTargetsMissingFromSnapshot(t *testing.T) {
	cm := setupModelsConfigManager(t, config.Config{Upstream: []config.UpstreamConfig{
		{Name: "first", BaseURL: "https://first.invalid", Status: "active", APIKeys: []string{"test"}, ServiceType: "claude", SupportedModels: []string{"unrelated"}},
		{Name: "third", BaseURL: "https://third.invalid", Status: "active", APIKeys: []string{"test"}, ServiceType: "claude", SupportedModels: []string{"target"}, ModelCapabilities: map[string]config.UpstreamModelCapability{"target": {ContextWindowTokens: 200000}}},
	}, ClientModelMappings: config.ClientModelMappings{"claude-desktop": {"sonnet": "target"}}})
	cfg := cm.GetConfig()
	router := httpbridge.NewApp()
	handler := func(c fiber.Ctx) error {
		result := withClientModelAliases(c, cm, buildModelsResponse([]ModelEntry{{ID: "unrelated"}}))
		httpbridge.JSON(c, 200, result)
		return nil
	}
	router.Get("/v1/models", handler)
	router.Get("/:routePrefix/v1/models", handler)
	for _, tc := range []struct {
		path, pin string
		found     bool
	}{
		{"/v1/models", "", true}, {"/v1/models", cfg.Upstream[1].Name, true}, {"/v1/models", cfg.Upstream[0].Name, false}, {"/private/v1/models", "", false},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Header.Set("X-Channel", tc.pin)
		rec := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(rec, req)
		var result ModelsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, model := range result.Data {
			if model.ID == "yunqi-claude-desktop-sonnet" {
				found = true
				if model.ContextWindow != 200000 || model.Supports1M {
					t.Fatalf("configured target capabilities: %+v", model)
				}
			}
		}
		if found != tc.found {
			t.Fatalf("path=%s pin=%s alias found=%v expected=%v", tc.path, tc.pin, found, tc.found)
		}
	}
}
