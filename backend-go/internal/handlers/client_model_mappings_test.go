package handlers

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
)

func TestClientMappingsManagementAuthenticationAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"upstream":[{"name":"DeepSeek","baseUrl":"https://example.invalid","apiKeys":["secret-upstream-key"],"serviceType":"claude","supportedModels":["deepseek-v4-pro"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cm, err := config.NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	router := httpbridge.NewApp()
	router.Use(middleware.WebAuthMiddleware(&config.EnvConfig{EnableWebUI: true, AdminAccessKey: "test-admin", ProxyAccessKey: "test-proxy"}, cm))
	router.Get("/api/console/client-model-mappings", GetClientModelMappings(cm))
	router.Put("/api/console/client-model-mappings", PutClientModelMappings(cm))
	for _, tc := range []struct {
		method, key, body string
		status            int
	}{
		{"GET", "", "", 401}, {"GET", "test-proxy", "", 401}, {"PUT", "test-proxy", `{"mappings":{}}`, 401},
		{"GET", "test-admin", "", 200}, {"PUT", "test-admin", `{}`, 400},
		{"PUT", "test-admin", `{"mappings":{"claude-desktop":{"haiku":"unavailable"}}}`, 400},
		{"PUT", "test-admin", `{"mappings":{"claude-desktop":{"haiku":"deepseek-v4-pro","opus":"deepseek-v4-pro"}}}`, 200},
		{"PUT", "test-admin", `{"mappings":{}}`, 200},
	} {
		req := httptest.NewRequest(tc.method, "/api/console/client-model-mappings", strings.NewReader(tc.body))
		req.Header.Set("x-api-key", tc.key)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.body, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "secret-upstream-key") {
			t.Fatal("management leaked upstream credentials")
		}
		if tc.status == 200 && rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("management response cached")
		}
	}
}
