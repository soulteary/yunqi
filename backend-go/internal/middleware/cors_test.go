package middleware

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupRouterWithCORS(envCfg *config.EnvConfig) *fiber.App {
	r := httpbridge.NewApp()
	r.Use(CORSMiddleware(envCfg))
	r.Use(WebAuthMiddleware(envCfg, nil))
	r.Get("/api/channels", func(c fiber.Ctx) error { httpbridge.JSON(c, http.StatusOK, fiber.Map{"ok": true}); return nil })
	return r
}

func TestCORSMiddlewareOriginPolicy(t *testing.T) {
	const trusted = "https://console.example"
	tests := []struct {
		name                                string
		enabled                             bool
		env, configured, origin, wantOrigin string
		credentials                         bool
	}{
		{name: "disabled ignores wildcard", configured: "*", origin: "https://untrusted.example"},
		{name: "disabled ignores configured origin", configured: trusted, origin: trusted},
		{name: "disabled ignores development exception", env: "development", configured: trusted, origin: "http://localhost:5173"},
		{name: "explicit origin", enabled: true, configured: trusted, origin: trusted, wantOrigin: trusted, credentials: true},
		{name: "reject unconfigured origin", enabled: true, configured: trusted, origin: "https://untrusted.example"},
		{name: "reject origin suffix", enabled: true, configured: trusted, origin: trusted + ".evil.example"},
		{name: "wildcard", enabled: true, configured: "*", origin: "https://untrusted.example", wantOrigin: "*"},
		{name: "empty policy", enabled: true, origin: trusted},
		{name: "development localhost", enabled: true, env: "development", configured: trusted, origin: "http://localhost:5173", wantOrigin: "http://localhost:5173", credentials: true},
		{name: "development localhost HTTPS", enabled: true, env: "development", configured: trusted, origin: "https://localhost", wantOrigin: "https://localhost", credentials: true},
		{name: "development localhost attacker domain", enabled: true, env: "development", configured: trusted, origin: "https://localhost.evil.example"},
		{name: "development localhost userinfo", enabled: true, env: "development", configured: trusted, origin: "https://localhost@evil.example"},
		{name: "development localhost path", enabled: true, env: "development", configured: trusted, origin: "https://evil.example/localhost"},
		{name: "development malformed origin", enabled: true, env: "development", configured: trusted, origin: "http://localhost:invalid"},
		{name: "development non HTTP origin", enabled: true, env: "development", configured: trusted, origin: "ftp://localhost"},
		{name: "development localhost path is not an origin", enabled: true, env: "development", configured: trusted, origin: "http://localhost/path"},
		{name: "production rejects localhost exception", enabled: true, env: "production", configured: trusted, origin: "http://localhost:5173"},
		{name: "no origin", enabled: true, configured: "*"},
	}
	for _, tt := range tests {
		for _, method := range []string{http.MethodOptions, http.MethodGet} {
			t.Run(tt.name+"/"+method, func(t *testing.T) {
				r := setupRouterWithCORS(&config.EnvConfig{EnableCORS: tt.enabled, CORSOrigin: tt.configured, Env: tt.env, EnableWebUI: true, ProxyAccessKey: "test-key"})
				req := httptest.NewRequest(method, "/api/channels", nil)
				if tt.origin != "" {
					req.Header.Set("Origin", tt.origin)
				}
				wantStatus := http.StatusNoContent
				if method == http.MethodOptions {
					req.Header.Set("Access-Control-Request-Method", http.MethodGet)
					req.Header.Set("Access-Control-Request-Headers", "x-api-key")
				} else {
					req.Header.Set("x-api-key", "test-key")
					wantStatus = http.StatusOK
				}
				w := httptest.NewRecorder()
				httpbridge.Handler(r).ServeHTTP(w, req)
				if w.Code != wantStatus {
					t.Fatalf("status=%d, want %d", w.Code, wantStatus)
				}
				if got := w.Header().Get("Access-Control-Allow-Origin"); got != tt.wantOrigin {
					t.Errorf("allowed origin=%q, want %q", got, tt.wantOrigin)
				}
				wantCredentials := ""
				if tt.credentials {
					wantCredentials = "true"
				}
				if got := w.Header().Get("Access-Control-Allow-Credentials"); got != wantCredentials {
					t.Errorf("credentials=%q, want %q", got, wantCredentials)
				}
				for _, header := range []string{"Access-Control-Allow-Methods", "Access-Control-Allow-Headers"} {
					if got := w.Header().Get(header); (got != "") != (tt.wantOrigin != "") {
						t.Errorf("%s=%q, should only be set for allowed origins", header, got)
					}
				}
				if tt.enabled && !strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "Origin") {
					t.Error("enabled origin policy must vary by Origin, including rejected and absent origins")
				}
			})
		}
	}
}

func TestCORSMiddlewareDoesNotBypassAuthentication(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		r := setupRouterWithCORS(&config.EnvConfig{EnableCORS: enabled, CORSOrigin: "https://console.example", EnableWebUI: true, ProxyAccessKey: "test-key"})
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		req.Header.Set("Origin", "https://console.example")
		w := httptest.NewRecorder()
		httpbridge.Handler(r).ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("enabled=%t: unauthenticated status=%d, want 401", enabled, w.Code)
		}
		if enabled && w.Header().Get("Access-Control-Allow-Origin") != "https://console.example" {
			t.Error("allowed clients must be able to read authentication errors")
		}
	}
}
