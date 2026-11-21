package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
)

func TestConsoleStatusManagementAuthAndNoSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"upstream":[],"chatUpstream":[],"responsesUpstream":[],"geminiUpstream":[],"imagesUpstream":[],"vectorsUpstream":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := config.NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	env := &config.EnvConfig{EnableWebUI: true, Port: 3688, Env: "production", ProxyAccessKey: "proxy-only-test-key", AdminAccessKey: "admin-only-test-key"}
	router := httpbridge.NewApp()
	router.Use(middleware.WebAuthMiddleware(env, manager))
	router.Get("/api/console/status", ConsoleStatus(env, manager))
	for _, tc := range []struct {
		key    string
		status int
	}{{"", 401}, {"proxy-only-test-key", 401}, {"admin-only-test-key", 200}} {
		req := httptest.NewRequest("GET", "/api/console/status", nil)
		req.Header.Set("x-api-key", tc.key)
		response := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(response, req)
		if response.Code != tc.status {
			t.Fatalf("status %d, expected %d", response.Code, tc.status)
		}
		if response.Code == 200 {
			body := response.Body.String()
			if strings.Contains(body, "only-test-key") || strings.Contains(body, path) {
				t.Fatal("diagnostic response leaked secrets or paths")
			}
			var data struct {
				Channels map[string]int `json:"channels"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			if len(data.Channels) != 6 {
				t.Fatalf("expected all six protocols, got %v", data.Channels)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("diagnostics must not be cached")
			}
		}
	}
}
