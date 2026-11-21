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

func TestCodexAutoReviewManagementAuthenticationAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"upstream":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := config.NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	router := httpbridge.NewApp()
	router.Use(middleware.WebAuthMiddleware(&config.EnvConfig{EnableWebUI: true, AdminAccessKey: "admin-test", ProxyAccessKey: "proxy-test"}, manager))
	router.Get("/api/console/codex-auto-review", GetCodexAutoReviewMapping(manager))
	router.Put("/api/console/codex-auto-review", PutCodexAutoReviewMapping(manager))
	for _, tc := range []struct {
		method, key, body string
		status            int
	}{
		{"GET", "", "", 401}, {"GET", "proxy-test", "", 401},
		{"PUT", "proxy-test", `{"enabled":true,"targetModel":"review-model"}`, 401},
		{"GET", "admin-test", "", 200},
		{"PUT", "admin-test", `{"targetModel":"review-model"}`, 400},
		{"PUT", "admin-test", `{"enabled":true,"targetModel":"codex-auto-review"}`, 400},
		{"PUT", "admin-test", `{"enabled":true,"targetModel":""}`, 400},
		{"PUT", "admin-test", `{"enabled":true,"targetModel":"review-model"}`, 200},
		{"GET", "admin-test", "", 200},
		{"PUT", "admin-test", `{"enabled":false,"targetModel":"review-model"}`, 200},
	} {
		request := httptest.NewRequest(tc.method, "/api/console/codex-auto-review", strings.NewReader(tc.body))
		request.Header.Set("x-api-key", tc.key)
		request.Header.Set("Content-Type", "application/json")
		writer := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(writer, request)
		if writer.Code != tc.status {
			t.Fatalf("%s %s: status=%d body=%s", tc.method, tc.body, writer.Code, writer.Body.String())
		}
		if tc.status == 200 && writer.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("management state must not be cached")
		}
	}
	if got := manager.GetCodexAutoReviewMapping(); got.Enabled || got.TargetModel != "review-model" {
		t.Fatalf("disabled state=%#v", got)
	}
}
