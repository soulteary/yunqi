package handlers

import (
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"net/http/httptest"
	"strings"
	"testing"
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
