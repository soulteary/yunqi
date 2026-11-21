package middleware

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"net/http/httptest"
	"testing"
)

func TestAdminBodyLogPermissionsFailClosed(t *testing.T) {
	for _, role := range []string{"viewer", "operator", "admin"} {
		w := httptest.NewRecorder()
		c, _ := httpbridge.TestContext(w)
		SetAdminPrincipal(c, AdminPrincipal{ID: "actor", Role: role, ChannelUIDs: []string{"owned"}})
		if CanAdminAction(c, "logs.body.download", "other") || CanAdminAction(c, "logs.body.configure", "") {
			t.Fatalf("%s escaped resource scope", role)
		}
		if got := CanAdminAction(c, "logs.body.download", "owned"); got != (role == "admin") {
			t.Fatalf("%s body download=%v", role, got)
		}
		if GetAdminActorID(c) != "actor" {
			t.Fatal("missing stable actor")
		}
	}
}

func TestManagedPrimaryDoesNotBecomeImplicitOwnerWithExtraProxyKeys(t *testing.T) {
	env := &config.EnvConfig{EnableWebUI: true, ProxyAccessKey: "primary", ExtraProxyAccessKeys: []string{"extra"}, ManagedAdminKeyLookup: func(raw string) (config.AdminCredential, bool) {
		return config.AdminCredential{ID: "viewer", ActorID: "viewer", Role: "viewer"}, raw == "primary"
	}}
	app := httpbridge.NewApp()
	app.Use(WebAuthMiddleware(env, nil))
	app.Get("/api/secrets", func(c fiber.Ctx) error { httpbridge.Text(c, 200, "secret"); return nil })
	req := httptest.NewRequest("GET", "/api/secrets", nil)
	req.Header.Set("x-api-key", "primary")
	w := httptest.NewRecorder()
	httpbridge.Handler(app).ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("managed viewer escalated via primary key: %d", w.Code)
	}
}
