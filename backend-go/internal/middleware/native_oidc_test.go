package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

type nativeBackendStub struct{}

func (nativeBackendStub) Lookup(context.Context, string) (config.NativeIdentitySession, error) {
	return config.NativeIdentitySession{SubjectID: "hashed-subject", Groups: []string{"team"}, ExpiresAt: time.Now().Add(time.Hour).Unix(), Fingerprint: config.EntryKeyHash("session")}, nil
}
func (nativeBackendStub) Close() error { return nil }
func TestNativeOIDCSessionUsesCurrentRoleAndCSRFPolicy(t *testing.T) {
	cfg := config.IdentityProxyConfig{GroupMappings: []config.IdentityGroupMapping{{Group: "team", Role: "operator", ChannelUIDs: []string{"one"}}}}
	env := &config.EnvConfig{EnableWebUI: true, AdminAccessKey: "owner", NativeOIDCIssuer: "https://idp.example", NativeOIDCRedirectURL: "https://console.example/auth/oidc/callback", NativeIdentity: nativeBackendStub{}, IdentityProxyConfigLookup: func() config.IdentityProxyConfig { return cfg }}
	router := httpbridge.NewApp()
	router.Use(WebAuthMiddleware(env, nil))
	router.Get("/api/access/me", func(c fiber.Ctx) error { httpbridge.JSON(c, 200, GetAdminPrincipal(c)); return nil })
	router.Put("/api/access/channels/:uid/state", func(c fiber.Ctx) error { httpbridge.Status(c, 200); return nil })
	call := func(method, path, origin, marker string) int {
		req := httptest.NewRequest(method, path, nil)
		req.AddCookie(&http.Cookie{Name: "__Host-yunqi-session", Value: "opaque"})
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if marker != "" {
			req.Header.Set(IdentityManagementHeader, marker)
		}
		rec := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(rec, req)
		return rec.Code
	}
	if got := call("GET", "/api/access/me", "", ""); got != 200 {
		t.Fatalf("session read %d", got)
	}
	for _, item := range []struct{ origin, marker string }{{"", "1"}, {"https://evil.example", "1"}, {"https://console.example", ""}} {
		if got := call("PUT", "/api/access/channels/one/state", item.origin, item.marker); got != 401 {
			t.Fatalf("CSRF accepted %d", got)
		}
	}
	if got := call("PUT", "/api/access/channels/one/state", "https://console.example", "1"); got != 200 {
		t.Fatalf("scoped operator rejected %d", got)
	}
	if got := call("PUT", "/api/access/channels/two/state", "https://console.example", "1"); got != 403 {
		t.Fatalf("scope expanded %d", got)
	}
	cfg.GroupMappings[0].Role = "viewer"
	if got := call("PUT", "/api/access/channels/one/state", "https://console.example", "1"); got != 403 {
		t.Fatalf("stale role survived mapping change %d", got)
	}
	cfg.GroupMappings = nil
	if got := call("GET", "/api/access/me", "", ""); got != 401 {
		t.Fatalf("removed group authenticated %d", got)
	}
}
