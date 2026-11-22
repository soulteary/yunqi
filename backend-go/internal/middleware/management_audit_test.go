package middleware

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagementAuditHTTPIntentCommitOutcomeAndRejectedActor(t *testing.T) {
	cm, err := config.NewConfigManager(filepath.Join(t.TempDir(), "config.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	env := &config.EnvConfig{EnableWebUI: true, AdminAccessKey: "owner", ManagedAdminKeyLookup: func(key string) (config.AdminCredential, bool) {
		return config.AdminCredential{ActorID: "reader", Role: "viewer"}, key == "viewer"
	}}
	router := httpbridge.NewApp()
	router.Use(ManagementAuditMiddleware(cm))
	router.Use(WebAuthMiddleware(env, cm))
	called := 0
	router.Put("/api/racing/config", func(c fiber.Ctx) error {
		called++
		audit := config.ManagementAuditFromContext(httpbridge.Request(c).Context())
		if err := cm.SetRacingEnabledAudited(audit, !cm.GetRacingEnabled()); err != nil {
			return err
		}
		httpbridge.JSON(c, 200, fiber.Map{"ok": true})
		return nil
	})
	router.Post("/api/task", func(c fiber.Ctx) error { httpbridge.JSON(c, 202, fiber.Map{"accepted": true}); return nil })
	router.Put("/api/bad", func(c fiber.Ctx) error { return fiber.NewError(400) })
	call := func(method, path, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("x-api-key", key)
		rec := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(rec, req)
		return rec
	}
	response := call("PUT", "/api/racing/config", "owner")
	if response.Code != 200 {
		t.Fatalf("update %d %s", response.Code, response.Body.String())
	}
	op := response.Header().Get("X-Yunqi-Audit-Operation")
	if op == "" {
		t.Fatal("missing audit correlation header")
	}
	outcomes := map[string]bool{}
	for _, event := range cm.ManagementAuditSnapshot().Events {
		if event.OperationID == op {
			outcomes[event.Outcome] = true
			if event.ActorID != "owner:"+config.EntryKeyHash("owner") {
				t.Fatal("wrong owner actor")
			}
		}
	}
	if !outcomes["intent"] || !outcomes["committed"] || !outcomes["completed"] {
		t.Fatal("missing durable request or commit phases", outcomes)
	}
	response = call("PUT", "/api/racing/config", "viewer")
	if response.Code != 403 || called != 1 {
		t.Fatal("viewer mutation ran")
	}
	op = response.Header().Get("X-Yunqi-Audit-Operation")
	rejected := false
	for _, event := range cm.ManagementAuditSnapshot().Events {
		if event.OperationID == op && event.Outcome == "rejected" && event.ActorID == "admin:reader" {
			rejected = true
		}
	}
	if !rejected {
		t.Fatal("permission denial not attributed")
	}
	response = call("POST", "/api/task", "owner")
	op = response.Header().Get("X-Yunqi-Audit-Operation")
	for _, event := range cm.ManagementAuditSnapshot().Events {
		if event.OperationID == op && (event.Outcome == "completed" || event.Outcome == "committed") {
			t.Fatal("accepted async request was reported completed")
		}
	}
	response = call("PUT", "/api/bad", "owner")
	op = response.Header().Get("X-Yunqi-Audit-Operation")
	failed := false
	for _, event := range cm.ManagementAuditSnapshot().Events {
		if event.OperationID == op && event.Outcome == "failed" && event.Status == 400 {
			failed = true
		}
	}
	if !failed {
		t.Fatal("handler error lost actual status")
	}
	env.NativeOIDCIssuer = "https://idp.example"
	env.NativeOIDCRedirectURL = "https://console.example/auth/oidc/callback"
	env.NativeIdentity = nativeBackendStub{}
	env.IdentityProxyConfigLookup = func() config.IdentityProxyConfig {
		return config.IdentityProxyConfig{GroupMappings: []config.IdentityGroupMapping{{Group: "team", Role: "admin"}}}
	}
	request := httptest.NewRequest("PUT", "/api/racing/config", nil)
	request.AddCookie(&http.Cookie{Name: "__Host-yunqi-session", Value: "opaque"})
	request.Header.Set("Origin", "https://console.example")
	request.Header.Set(IdentityManagementHeader, "1")
	nativeResponse := httptest.NewRecorder()
	httpbridge.Handler(router).ServeHTTP(nativeResponse, request)
	if nativeResponse.Code != 200 {
		t.Fatal("native identity mutation failed", nativeResponse.Code)
	}
	nativeOp := nativeResponse.Header().Get("X-Yunqi-Audit-Operation")
	nativeCommit := false
	for _, event := range cm.ManagementAuditSnapshot().Events {
		if event.OperationID == nativeOp && event.Outcome == "committed" {
			nativeCommit = true
			if event.ActorID != "sso:hashed-subject" {
				t.Fatal("native OIDC actor was lost")
			}
		}
	}
	if !nativeCommit {
		t.Fatal("native OIDC mutation had no committed audit")
	}
	before := cm.ManagementAuditSnapshot().NextSequence
	response = call("PUT", "/api/racing/config", "bad-key")
	if response.Code != 401 || cm.ManagementAuditSnapshot().NextSequence != before {
		t.Fatal("anonymous rejection triggered config writes or succeeded")
	}
	lookupCalls := 0
	env.ManagedAdminKeyLookup = func(key string) (config.AdminCredential, bool) {
		lookupCalls++
		return config.AdminCredential{ActorID: "rotating", Role: "admin"}, lookupCalls == 1
	}
	response = call("PUT", "/api/racing/config", "rotating")
	if response.Code != 403 {
		t.Fatal("revoked credential gained mutation access", response.Code)
	}
	unresolvedOp := response.Header().Get("X-Yunqi-Audit-Operation")
	resolved := false
	for _, event := range cm.ManagementAuditSnapshot().Events {
		if event.OperationID == unresolvedOp {
			if event.ActorID == "system" {
				t.Fatal("unresolved HTTP identity was attributed to background system")
			}
			if event.Outcome == "rejected" && event.ActorID == "unresolved_admin_identity" {
				resolved = true
			}
		}
	}
	if !resolved {
		t.Fatal("credential rotation denial lost unresolved actor audit")
	}

}
func TestManagementAuditCannotExecuteMutationWithoutDurableIntent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cm, err := config.NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	env := &config.EnvConfig{EnableWebUI: true, AdminAccessKey: "owner"}
	router := httpbridge.NewApp()
	router.Use(ManagementAuditMiddleware(cm))
	router.Use(WebAuthMiddleware(env, cm))
	called := false
	router.Put("/api/racing/config", func(c fiber.Ctx) error { called = true; return nil })
	// Keep the manager valid but make its backing file unavailable.
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("PUT", "/api/racing/config", strings.NewReader("{}"))
	req.Header.Set("x-api-key", "owner")
	rec := httptest.NewRecorder()
	httpbridge.Handler(router).ServeHTTP(rec, req)
	if rec.Code != 503 || called {
		t.Fatal("mutation ran without durable intent", rec.Code)
	}
}
