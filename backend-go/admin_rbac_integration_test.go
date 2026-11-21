package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestAdminRolesProtectLegacySecretsAndScopedOperations(t *testing.T) {
	a := newRouteTestApplication(t, true, "production")
	for _, uid := range []string{"ch_owned", "ch_other"} {
		if err := a.cfgManager.AddChatUpstream(config.UpstreamConfig{ChannelUID: uid, Name: uid, BaseURL: "https://private.example.invalid", APIKeys: []string{"secret-provider-key"}, Status: "active", ServiceType: "openai"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"viewer", "operator", "admin"} {
		if err := a.cfgManager.PutAdminCredential(config.AdminCredential{ID: role, KeyHash: config.EntryKeyHash("key-" + role), ActorID: "actor-" + role, Role: role, ChannelUIDs: []string{"ch_owned"}}, ""); err != nil {
			t.Fatal(err)
		}
	}
	handler := httpbridge.Handler(a.newRouter())
	invoke := func(method, path, key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("x-api-key", key)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	for _, role := range []string{"viewer", "operator", "admin"} {
		w := invoke("GET", "/api/access/channels", "key-"+role, "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "ch_owned") || strings.Contains(w.Body.String(), "ch_other") || strings.Contains(w.Body.String(), "secret-provider-key") || strings.Contains(w.Body.String(), "private.example") {
			t.Fatalf("unsafe scoped inventory %s: %d %s", role, w.Code, w.Body.String())
		}
		for _, path := range []string{"/api/chat/channels", "/api/channels/export", "/api/entry-keys", "/api/admin-credentials", "/api/reports/request-timeline/req_example"} {
			if got := invoke("GET", path, "key-"+role, ""); got.Code != 403 {
				t.Fatalf("legacy bypass %s %s: %d %s", role, path, got.Code, got.Body.String())
			}
		}
		if got := invoke("PUT", "/api/access/channels/ch_other/state", "key-"+role, `{"status":"disabled"}`); got.Code != 403 {
			t.Fatalf("cross-scope write %s: %d", role, got.Code)
		}
	}
	if got := invoke("PUT", "/api/access/channels/ch_owned/state", "key-viewer", `{"status":"disabled"}`); got.Code != 403 {
		t.Fatalf("viewer wrote state: %d", got.Code)
	}
	if got := invoke("PUT", "/api/access/channels/ch_owned/state", "key-operator", `{"status":"disabled"}`); got.Code != 200 {
		t.Fatalf("operator state %d: %s", got.Code, got.Body.String())
	}
	cfg := a.cfgManager.GetConfig()
	for _, up := range cfg.ChatUpstream {
		if up.ChannelUID == "ch_owned" && config.GetChannelAdminState(&up) != "disabled" {
			t.Fatal("state change did not reach live configuration")
		}
		if up.ChannelUID == "ch_other" && config.GetChannelAdminState(&up) != "active" {
			t.Fatalf("unowned channel changed: %+v", up)
		}
	}
	if got := invoke("POST", "/v1/chat/completions", "key-admin", `{"model":"test","messages":[]}`); got.Code != 401 {
		t.Fatalf("management key implicitly granted proxy: %d", got.Code)
	}
	w := invoke("POST", "/api/admin-credentials/operator/rotate", "route-admin", `{}`)
	if w.Code != 201 {
		t.Fatalf("rotate: %d %s", w.Code, w.Body.String())
	}
	var rotated struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}
	if got := invoke("GET", "/api/access/me", "key-operator", ""); got.Code != 401 {
		t.Fatalf("old management key valid: %d", got.Code)
	}
	w = invoke("GET", "/api/access/me", rotated.Key, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "actor-operator") {
		t.Fatalf("rotation lost actor: %d %s", w.Code, w.Body.String())
	}
}
