package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// setupRouterWithAuth builds a minimal router with the auth middleware wired.
func setupRouterWithAuth(envCfg *config.EnvConfig) *fiber.App {

	r := httpbridge.NewApp()
	r.Use(WebAuthMiddleware(envCfg, nil))

	// Protected management API
	r.Get("/api/channels", func(c fiber.Ctx) error {
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"ok": true})
		return nil
	})

	// Protected admin endpoint
	r.Post("/admin/config/save", func(c fiber.Ctx) error {
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"ok": true})
		return nil
	})
	r.Get("/admin/dev/info", func(c fiber.Ctx) error {
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"ok": true})
		return nil
	})

	// SPA routes should pass through without access key
	r.Get("/", func(c fiber.Ctx) error {
		httpbridge.Text(c, http.StatusOK, "home")
		return nil
	})
	r.Get("/dashboard", func(c fiber.Ctx) error {
		httpbridge.Text(c, http.StatusOK, "dashboard")
		return nil
	})

	return r
}

func setupRouterWithProxyAuth(envCfg *config.EnvConfig) *fiber.App {

	r := httpbridge.NewApp()
	r.Use(ProxyAuthMiddleware(envCfg))
	r.Post("/v1/messages", func(c fiber.Ctx) error {
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"ok": true})
		return nil
	})
	return r
}

func TestWebAuthMiddleware_APIRequiresKey(t *testing.T) {
	envCfg := &config.EnvConfig{
		ProxyAccessKey: "secret-key",
		EnableWebUI:    true,
	}
	router := setupRouterWithAuth(envCfg)

	t.Run("missing key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		w := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("wrong key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		req.Header.Set("x-api-key", "wrong")
		w := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("correct key allows access", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		req.Header.Set("x-api-key", envCfg.ProxyAccessKey)
		w := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

// TestWebAuthMiddleware_SecWebSocketProtocolFallback 验证浏览器原生 WebSocket
// 无法设置自定义 header 时，可通过 Sec-WebSocket-Protocol 子协议传递鉴权 key
// （Phase 3A：/api/health-center/events 的鉴权基础）。
func TestWebAuthMiddleware_SecWebSocketProtocolFallback(t *testing.T) {
	envCfg := &config.EnvConfig{
		ProxyAccessKey: "secret-key",
		EnableWebUI:    true,
	}
	router := setupRouterWithAuth(envCfg)

	t.Run("valid key via Sec-WebSocket-Protocol allows access", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		req.Header.Set("Sec-WebSocket-Protocol", envCfg.ProxyAccessKey)
		w := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("invalid key via Sec-WebSocket-Protocol returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		req.Header.Set("Sec-WebSocket-Protocol", "wrong")
		w := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("x-api-key takes priority over Sec-WebSocket-Protocol", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		req.Header.Set("x-api-key", envCfg.ProxyAccessKey)
		req.Header.Set("Sec-WebSocket-Protocol", "garbage-that-would-fail")
		w := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (x-api-key 应优先生效)", w.Code, http.StatusOK)
		}
	})
}

func TestWebAuthMiddleware_SPAPassesThrough(t *testing.T) {
	envCfg := &config.EnvConfig{
		ProxyAccessKey: "secret-key",
		EnableWebUI:    true,
	}
	router := setupRouterWithAuth(envCfg)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	w := httptest.NewRecorder()
	httpbridge.Handler(router).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestWebAuthMiddleware_AdminRequiresKey(t *testing.T) {
	envCfg := &config.EnvConfig{
		ProxyAccessKey: "secret-key",
		EnableWebUI:    true,
	}
	router := setupRouterWithAuth(envCfg)

	t.Run("missing key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/config/save", nil)
		w := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("correct key allows access", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/admin/config/save", nil)
		req.Header.Set("x-api-key", envCfg.ProxyAccessKey)
		w := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestWebAuthMiddleware_DevInfoRequiresKeyInDevelopment(t *testing.T) {
	envCfg := &config.EnvConfig{
		Env:            "development",
		ProxyAccessKey: "secret-key",
		EnableWebUI:    true,
	}
	router := setupRouterWithAuth(envCfg)

	t.Run("missing key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/dev/info", nil)
		w := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("correct key allows access", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/dev/info", nil)
		req.Header.Set("x-api-key", envCfg.ProxyAccessKey)
		w := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestWebAuthMiddleware_AllowsV1BetaRoutesWhenWebUIDisabled(t *testing.T) {
	envCfg := &config.EnvConfig{
		ProxyAccessKey: "secret-key",
		EnableWebUI:    false,
	}

	r := httpbridge.NewApp()
	r.Use(WebAuthMiddleware(envCfg, nil))

	r.Post("/v1beta/models/*", func(c fiber.Ctx) error {
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"ok": true})
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.0-flash:generateContent", nil)
	w := httptest.NewRecorder()
	httpbridge.Handler(r).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestWebAuthMiddleware_ExtraProxyKeysDoNotGrantAdminAccess(t *testing.T) {
	envCfg := &config.EnvConfig{
		ProxyAccessKey:       "primary-key",
		ExtraProxyAccessKeys: []string{"extra-key"},
		AdminAccessKey:       "admin-key",
		EnableWebUI:          true,
	}
	router := setupRouterWithAuth(envCfg)

	t.Run("extra proxy key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		req.Header.Set("x-api-key", "extra-key")
		w := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("primary proxy key returns 401 when extra keys exist", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		req.Header.Set("x-api-key", "primary-key")
		w := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("admin key allows access", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
		req.Header.Set("x-api-key", "admin-key")
		w := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestProxyAuthMiddleware_AllowsExtraProxyKeys(t *testing.T) {
	envCfg := &config.EnvConfig{
		ProxyAccessKey:       "primary-key",
		ExtraProxyAccessKeys: []string{"extra-a", "extra-b"},
	}
	router := setupRouterWithProxyAuth(envCfg)

	tests := []struct {
		name       string
		headerName string
		key        string
		wantStatus int
	}{
		{name: "primary x-api-key", headerName: "x-api-key", key: "primary-key", wantStatus: http.StatusOK},
		{name: "extra authorization bearer", headerName: "Authorization", key: "Bearer extra-a", wantStatus: http.StatusOK},
		{name: "extra gemini header", headerName: "x-goog-api-key", key: "extra-b", wantStatus: http.StatusOK},
		{name: "wrong key", headerName: "x-api-key", key: "wrong", wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			req.Header.Set(tt.headerName, tt.key)
			w := httptest.NewRecorder()
			httpbridge.Handler(router).ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestProxyAuthMiddleware_WritesProxyKeyMask(t *testing.T) {
	envCfg := &config.EnvConfig{
		ProxyAccessKey:       "sk-primarykey-aaaa",
		ExtraProxyAccessKeys: []string{"sk-extrakey-bbbb"},
	}

	r := httpbridge.NewApp()
	r.Use(ProxyAuthMiddleware(envCfg))

	var capturedMask string
	r.Post("/v1/messages", func(c fiber.Ctx) error {
		capturedMask = GetProxyKeyMask(c)
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"ok": true})
		return nil
	})

	t.Run("primary key sets mask", func(t *testing.T) {
		capturedMask = ""
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		req.Header.Set("x-api-key", "sk-primarykey-aaaa")
		w := httptest.NewRecorder()
		httpbridge.Handler(r).ServeHTTP(w, req)

		if capturedMask == "" {
			t.Fatal("expected non-empty proxyKeyMask for primary key")
		}
		// Should be a masked version (first 4 + last 4 with stars in between)
		if len(capturedMask) < 8 {
			t.Fatalf("expected mask length >= 8, got %q", capturedMask)
		}
	})

	t.Run("extra key sets mask", func(t *testing.T) {
		capturedMask = ""
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		req.Header.Set("x-api-key", "sk-extrakey-bbbb")
		w := httptest.NewRecorder()
		httpbridge.Handler(r).ServeHTTP(w, req)

		if capturedMask == "" {
			t.Fatal("expected non-empty proxyKeyMask for extra key")
		}
	})

	t.Run("no key means no mask", func(t *testing.T) {
		capturedMask = "sentinel"
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		w := httptest.NewRecorder()
		httpbridge.Handler(r).ServeHTTP(w, req)

		// Middleware returns 401 and aborts; handler never runs.
		// capturedMask stays "sentinel" because the handler is never called.
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
		if capturedMask != "sentinel" {
			t.Fatalf("handler should not have run; expected sentinel, got %q", capturedMask)
		}
	})
}

func TestGetProxyKeyMask_ReturnsEmptyWhenNotSet(t *testing.T) {

	c, _ := httpbridge.TestContext(httptest.NewRecorder())
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodGet, "/", nil))

	if mask := GetProxyKeyMask(c); mask != "" {
		t.Fatalf("expected empty mask for fresh context, got %q", mask)
	}
}
