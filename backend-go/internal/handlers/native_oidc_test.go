package handlers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNativeOIDCPortalAndCallbackAreClosedByDefault(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		env := &config.EnvConfig{EnableWebUI: true}
		if enabled {
			env.NativeOIDCIssuer = "http://invalid.example"
		}
		router := httpbridge.NewApp()
		RegisterNativeOIDCRoutes(router, env)
		router.Get("/access", AccessPortal(env))
		handler := httpbridge.Handler(router)
		portal := httptest.NewRecorder()
		handler.ServeHTTP(portal, httptest.NewRequest("GET", "/access", nil))
		if strings.Contains(portal.Body.String(), "请通过配置的身份代理登录后重试") || strings.Contains(portal.Body.String(), "其它节点请在身份代理退出") {
			t.Fatal("native portal mislabels the identity boundary")
		}
		if portal.Code != 200 || strings.Contains(portal.Body.String(), "__NATIVE_OIDC_ENABLED__") || !strings.Contains(portal.Body.String(), "const nativeOIDCEnabled=false;") {
			t.Fatal("portal advertised unavailable native login")
		}
		callback := httptest.NewRecorder()
		handler.ServeHTTP(callback, httptest.NewRequest("GET", "/auth/oidc/callback?code=secret-code&state=forged", nil))
		if callback.Code < 400 || strings.Contains(callback.Body.String(), "secret-code") {
			t.Fatal("callback exposed code or accepted invalid config")
		}
	}
}

func TestNativeOIDCAdminRevocationOnlyClearsTargetedBrowserCookie(t *testing.T) {
	for _, self := range []bool{false, true} {
		t.Run(strconv.FormatBool(self), func(t *testing.T) {
			cm, err := config.NewConfigManager(filepath.Join(t.TempDir(), "config.json"), "")
			if err != nil {
				t.Fatal(err)
			}
			defer cm.Close()
			if err = cm.SetIdentityProxyConfig(config.IdentityProxyConfig{}); err != nil {
				t.Fatal(err)
			}
			own := config.EntryKeyHash("own-session")
			target := config.EntryKeyHash("another-session")
			if self {
				target = own
			}
			router := httpbridge.NewApp()
			router.Use(func(c fiber.Ctx) error {
				middleware.SetAdminPrincipal(c, middleware.AdminPrincipal{ID: "admin:owner", Role: "admin", Source: "oidc", SessionFingerprint: own, SessionExpiresAt: time.Now().Add(time.Hour).Unix()})
				return c.Next()
			})
			RegisterIdentityProxyRoutes(router, &config.EnvConfig{}, cm)
			req := httptest.NewRequest("POST", "/identity-proxy/sessions/revoke", strings.NewReader(`{"sessionFingerprint":"`+target+`"}`))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			httpbridge.Handler(router).ServeHTTP(response, req)
			if response.Code != 200 {
				t.Fatalf("revoke %d %s", response.Code, response.Body.String())
			}
			cleared := false
			for _, cookie := range response.Result().Cookies() {
				if cookie.Name == "__Host-yunqi-session" && cookie.MaxAge < 0 {
					cleared = true
				}
			}
			if cleared != self {
				t.Fatalf("self=%t cookie-cleared=%t", self, cleared)
			}
		})
	}
}
