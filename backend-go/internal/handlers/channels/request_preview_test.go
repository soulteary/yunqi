package channels

import (
	"bytes"
	"encoding/json"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequestPreviewAuthorizesScopeAndNeverSavesDraft(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{ChatUpstream: []config.UpstreamConfig{{ChannelUID: "ch_preview", BaseURL: "https://example.test", ServiceType: "openai", APIKeys: []string{"unusual-sensitive-key"}}}}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cm, err := config.NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	before, _ := os.ReadFile(path)
	for _, tc := range []struct {
		principal middleware.AdminPrincipal
		status    int
	}{
		{middleware.AdminPrincipal{}, 403},
		{middleware.AdminPrincipal{ID: "viewer", Role: "viewer", ChannelUIDs: []string{"ch_other"}}, 403},
		{middleware.AdminPrincipal{ID: "viewer", Role: "viewer", ChannelUIDs: []string{"ch_preview"}}, 200},
	} {
		app := httpbridge.NewApp()
		app.Use(func(c fiber.Ctx) error { middleware.SetAdminPrincipal(c, tc.principal); return c.Next() })
		RegisterRoutes(app, cm, nil)
		request := httptest.NewRequest(http.MethodPost, "/channels/ch_preview/request-preview", strings.NewReader(`{"protocol":"chat","model":"test","body":{"model":"test","messages":[]},"draft":{"customHeaders":{"X-Auth":"draft-secret"},"textVerbosity":"high"}}`))
		recorder := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(recorder, request)
		if recorder.Code != tc.status {
			t.Fatalf("role %s status %d: %s", tc.principal.ID, recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "unusual-sensitive-key") || strings.Contains(recorder.Body.String(), "draft-secret") {
			t.Fatal("preview leaked credentials")
		}
		if tc.status == 200 && recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("preview can be cached")
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || cm.GetConfig().ChatUpstream[0].TextVerbosity != "" {
		t.Fatal("preview saved a draft")
	}
}
