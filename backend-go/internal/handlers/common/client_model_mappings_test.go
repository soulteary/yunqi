package common

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestClientModelMappingProtocolAndRequestBoundaries(t *testing.T) {
	up := config.UpstreamConfig{Status: "active", Name: "test", BaseURL: "https://example.invalid", APIKeys: []string{"test"}, ServiceType: "claude", SupportedModels: []string{"target"}}
	cfg := config.Config{Upstream: []config.UpstreamConfig{up}, ChatUpstream: []config.UpstreamConfig{up}, ResponsesUpstream: []config.UpstreamConfig{up}, ClientModelMappings: config.ClientModelMappings{
		"claude-desktop": {"haiku": "target"}, "claude-code": {"haiku": "target"}, "codex": {"default": "target"}, "opencode": {"default": "target", "small": "target"},
	}}
	data, _ := json.Marshal(cfg)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cm, err := config.NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	for _, tc := range []struct {
		alias, kind, path string
		status            int
	}{
		{"yunqi-claude-desktop-haiku", "messages", "/v1/test", 200},
		{"yunqi-claude-code-haiku", "messages", "/v1/test", 200},
		{"yunqi-codex-default", "responses", "/v1/test", 200},
		{"yunqi-opencode-default", "chat", "/v1/test", 200},
		{"yunqi-opencode-small", "chat", "/v1/test", 200},
		{"yunqi-claude-desktop-haiku", "chat", "/v1/test", 400},
		{"yunqi-claude-desktop-unknown", "messages", "/v1/test", 400},
		{"yunqi-claude-desktop-haiku", "messages", "/private/v1/test", 400},
		{"actual-model", "messages", "/v1/test", 200},
	} {
		t.Run(tc.alias+tc.kind+tc.path, func(t *testing.T) {
			router := httpbridge.NewApp()
			handler := func(c fiber.Ctx) error {
				body, err := ReadRequestBody(c, 1<<20)
				if err != nil {
					return nil
				}
				mapped, err := ApplyClientModelMapping(c, cm, body, tc.kind)
				if err != nil {
					return nil
				}
				httpbridge.Data(c, 200, "application/json", mapped)
				return nil
			}
			router.Post("/v1/test", handler)
			router.Post("/:routePrefix/v1/test", handler)
			req := httptest.NewRequest("POST", tc.path, strings.NewReader(`{"model":"`+tc.alias+`","stream":true,"messages":[{"role":"user","content":"unaltered"}]}`))
			rec := httptest.NewRecorder()
			httpbridge.Handler(router).ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d %s", rec.Code, rec.Body.String())
			}
			if tc.status == 200 {
				expected := "target"
				if tc.alias == "actual-model" {
					expected = tc.alias
				}
				if !strings.Contains(rec.Body.String(), `"model":"`+expected+`"`) || !strings.Contains(rec.Body.String(), `"stream":true`) || !strings.Contains(rec.Body.String(), "unaltered") {
					t.Fatal("request fields lost", rec.Body.String())
				}
			}
		})
	}
}
