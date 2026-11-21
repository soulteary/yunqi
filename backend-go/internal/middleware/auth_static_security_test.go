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

func TestWebAuthMiddleware_StaticLookingManagementPathsRemainProtected(t *testing.T) {
	paths := []string{
		"/api/channels/example.js",
		"/api/messages/channels/0/keys/example.css",
		"/api/channels/assets/example.svg",
		"/admin/config/example.png",
		"/api",
		"/admin",
	}
	credentials := []struct {
		name string
		key  string
		want int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"invalid", "wrong-key", http.StatusUnauthorized},
		{"proxy-only", "proxy-key", http.StatusUnauthorized},
		{"admin", "admin-key", http.StatusOK},
	}
	for _, webUI := range []bool{true, false} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
			for _, path := range paths {
				for _, credential := range credentials {
					t.Run(fmt.Sprintf("webUI=%t/%s/%s/%s", webUI, method, path, credential.name), func(t *testing.T) {
						env := &config.EnvConfig{EnableWebUI: webUI, ProxyAccessKey: "proxy-key", AdminAccessKey: "admin-key"}
						router := httpbridge.NewApp()
						router.Use(WebAuthMiddleware(env, nil))
						reached := false
						router.Add([]string{method}, path, func(c fiber.Ctx) error {
							reached = true
							httpbridge.JSON(c, http.StatusOK, fiber.Map{"ok": true})
							return nil
						})
						req := httptest.NewRequest(method, path, nil)
						if credential.key != "" {
							req.Header.Set("x-api-key", credential.key)
						}
						w := httptest.NewRecorder()
						httpbridge.Handler(router).ServeHTTP(w, req)
						want := credential.want
						if !webUI {
							want = http.StatusNotFound
						}
						if w.Code != want {
							t.Fatalf("status = %d, want %d; handler reached = %t", w.Code, want, reached)
						}
						if reached != (want == http.StatusOK) {
							t.Fatalf("handler reached = %t for status %d", reached, w.Code)
						}
					})
				}
			}
		}
	}
}

func TestWebAuthMiddleware_PublicStaticResourcesStillAccessible(t *testing.T) {
	for _, webUI := range []bool{true, false} {
		for _, path := range []string{"/assets/app.js", "/assets/styles.css", "/favicon.ico"} {
			t.Run(fmt.Sprintf("webUI=%t/%s", webUI, path), func(t *testing.T) {
				router := httpbridge.NewApp()
				router.Use(WebAuthMiddleware(&config.EnvConfig{EnableWebUI: webUI, ProxyAccessKey: "secret"}, nil))
				router.Get(path, func(c fiber.Ctx) error {
					httpbridge.Text(c, http.StatusOK, "static")
					return nil
				})
				w := httptest.NewRecorder()
				httpbridge.Handler(router).ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
				if w.Code != http.StatusOK {
					t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
				}
			})
		}
	}
}
