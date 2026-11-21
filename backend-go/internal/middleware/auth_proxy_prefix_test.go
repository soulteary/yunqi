package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestWebAuthMiddleware_PrefixedProxyRoutesInAPIMode(t *testing.T) {
	endpoints := []struct{ method, path string }{
		{"POST", "/v1/messages"}, {"POST", "/v1/messages/count_tokens"},
		{"GET", "/v1/models"}, {"GET", "/v1/models/example-model"},
		{"POST", "/v1/responses"}, {"GET", "/v1/responses"},
		{"POST", "/v1/responses/compact"}, {"POST", "/v1/alpha/history/v2/items"},
		{"POST", "/v1/alpha/notes/v2/items"}, {"POST", "/v1beta/models/gemini:generateContent"},
		{"POST", "/v1beta/models/gemini:streamGenerateContent"},
		{"POST", "/v1/chat/completions"}, {"POST", "/v1/images/generations"},
		{"POST", "/v1/images/edits"}, {"POST", "/v1/images/variations"}, {"POST", "/v1/embeddings"},
	}
	for _, web := range []bool{false, true} {
		for _, prefix := range []string{"", "/team", "/api", "/admin", "/api-team", "/administration"} {
			for _, endpoint := range endpoints {
				for _, key := range []string{"", "wrong-key", "proxy-key", "admin-key"} {
					path := prefix + endpoint.path
					t.Run(fmt.Sprintf("web=%t/%s/%s/key=%s", web, endpoint.method, path, key), func(t *testing.T) {
						env := &config.EnvConfig{EnableWebUI: web, ProxyAccessKey: "proxy-key", AdminAccessKey: "admin-key"}
						router := httpbridge.NewApp()
						router.Use(WebAuthMiddleware(env, nil))
						reached := false
						router.Add([]string{endpoint.method}, path, ProxyAuthMiddleware(env), func(c fiber.Ctx) error {
							reached = true
							httpbridge.Text(c, http.StatusOK, "proxy")
							return nil
						})
						req := httptest.NewRequest(endpoint.method, path, nil)
						if key != "" {
							req.Header.Set("x-api-key", key)
						}
						w := httptest.NewRecorder()
						httpbridge.Handler(router).ServeHTTP(w, req)
						want := http.StatusUnauthorized
						if key == "proxy-key" {
							want = http.StatusOK
						}
						if w.Code != want || reached != (want == http.StatusOK) {
							t.Fatalf("status=%d reached=%t, want status=%d", w.Code, reached, want)
						}
					})
				}
			}
		}
	}
}

func TestWebAuthMiddleware_PrefixedHealthInAPIMode(t *testing.T) {
	for _, path := range []string{"/health", "/team/health", "/api/health", "/admin/health", "/api-team/health"} {
		t.Run(path, func(t *testing.T) {
			router := httpbridge.NewApp()
			router.Use(WebAuthMiddleware(&config.EnvConfig{EnableWebUI: false}, nil))
			router.Get(path, func(c fiber.Ctx) error { httpbridge.Text(c, 200, "healthy"); return nil })
			w := httptest.NewRecorder()
			httpbridge.Handler(router).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != 200 {
				t.Fatalf("status=%d, want 200", w.Code)
			}
		})
	}
}

func TestWebAuthMiddleware_ProxyLookingManagementPathsStayProtected(t *testing.T) {
	for _, path := range []string{"/api/channels/v1/messages", "/admin/config/v1/responses", "/api/v1/messages/extra", "/admin/v1/models/model/extra", "/api/team/v1/messages", "/api/v1/chat/completions.js"} {
		t.Run(path, func(t *testing.T) {
			router := httpbridge.NewApp()
			router.Use(WebAuthMiddleware(&config.EnvConfig{EnableWebUI: true, ProxyAccessKey: "proxy-key", AdminAccessKey: "admin-key"}, nil))
			router.Post(path, func(c fiber.Ctx) error { t.Fatal("management handler reached without admin key"); return nil })
			req := httptest.NewRequest("POST", path, nil)
			req.Header.Set("x-api-key", "proxy-key")
			w := httptest.NewRecorder()
			httpbridge.Handler(router).ServeHTTP(w, req)
			if w.Code != 401 {
				t.Fatalf("status=%d, want 401", w.Code)
			}
		})
	}
}

func TestDataPlaneRequestRejectsInvalidRouteShapes(t *testing.T) {
	for _, tc := range []struct{ method, path string }{
		{"POST", "/one/two/v1/messages"}, {"POST", "//team/v1/messages"},
		{"POST", "/./v1/messages"}, {"POST", "/../v1/messages"},
		{"POST", "/api/v1/messages/extra"}, {"POST", "/api/v1/messages.js"},
		{"PUT", "/api/v1/responses"}, {"DELETE", "/admin/v1/models/model"},
		{"GET", "/team/v1/chat/completions"}, {"GET", "/api/v1/models/model/extra"},
		{"POST", "/api/health"}, {"GET", "/one/two/health"},
		{"POST", "/api/channels/v1/messages"}, {"POST", "/v1/unknown"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			if isDataPlaneRequest(tc.method, tc.path) {
				t.Fatal("invalid route shape accepted as data plane")
			}
		})
	}
}
