package main

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
)

func TestTrustedIdentityProxyRoleMappingAndSessionRevocation(t *testing.T) {
	a := newRouteTestApplication(t, true, "production")
	a.envCfg.IdentityProxySecret = strings.Repeat("s", 32)
	for _, uid := range []string{"ch_owned", "ch_other"} {
		if err := a.cfgManager.AddChatUpstream(config.UpstreamConfig{ChannelUID: uid, Name: uid, BaseURL: "https://private.invalid", APIKeys: []string{"private-key"}, ServiceType: "openai", Status: "active"}); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.IdentityProxyConfig{Enabled: true, Issuer: "team-proxy", AllowedOrigins: []string{"https://yunqi.example.invalid"}, TrustedCIDRs: []string{"10.0.0.0/24"}, GroupMappings: []config.IdentityGroupMapping{{Group: "operators", Role: "operator", ChannelUIDs: []string{"ch_owned"}}, {Group: "viewers", Role: "viewer", ChannelUIDs: []string{"ch_other"}}}}
	if err := a.cfgManager.SetIdentityProxyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	handler := httpbridge.Handler(a.newRouter())
	invoke := func(method, path string, mutate func(*httptest.ResponseRecorder), change func(map[string]string)) *httptest.ResponseRecorder {
		headers := map[string]string{"Origin": "https://yunqi.example.invalid", middleware.IdentityManagementHeader: "1", "X-Yunqi-Identity-Role": "admin", middleware.IdentitySecretHeader: a.envCfg.IdentityProxySecret, middleware.IdentitySubjectHeader: "person@example.invalid", middleware.IdentityGroupsHeader: "viewers,operators", middleware.IdentitySessionHeader: "external-session-id", middleware.IdentityExpiresHeader: strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)}
		if change != nil {
			change(headers)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(`{"status":"disabled"}`))
		req.RemoteAddr = "10.0.0.2:3000"
		req.Header.Set("Content-Type", "application/json")
		for name, value := range headers {
			if name == "RemoteAddr" {
				req.RemoteAddr = value
			} else {
				req.Header.Set(name, value)
			}
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if mutate != nil {
			mutate(w)
		}
		return w
	}
	w := invoke("GET", "/api/access/me", nil, nil)
	if w.Code != 200 {
		t.Fatalf("trusted identity %d %s", w.Code, w.Body.String())
	}
	var principal middleware.AdminPrincipal
	if err := json.Unmarshal(w.Body.Bytes(), &principal); err != nil {
		t.Fatal(err)
	}
	if principal.Role != "operator" || principal.Source != "identity_proxy" || len(principal.ChannelUIDs) != 1 || principal.ChannelUIDs[0] != "ch_owned" || principal.SessionFingerprint == "" {
		t.Fatalf("unsafe role union: %+v", principal)
	}
	if strings.Contains(w.Body.String(), "person@example") || strings.Contains(w.Body.String(), "external-session-id") {
		t.Fatal("raw subject/session leaked")
	}
	if w = invoke("GET", "/api/access/channels", nil, nil); w.Code != 200 || strings.Contains(w.Body.String(), "ch_other") || strings.Contains(w.Body.String(), "private-key") {
		t.Fatalf("scope leak: %d %s", w.Code, w.Body.String())
	}
	if w = invoke("PUT", "/api/access/channels/ch_owned/state", nil, nil); w.Code != 200 {
		t.Fatalf("scoped operation %d %s", w.Code, w.Body.String())
	}
	if w = invoke("GET", "/api/chat/channels", nil, nil); w.Code != 403 {
		t.Fatalf("SSO legacy privilege bypass: %d", w.Code)
	}
	if w = invoke("POST", "/v1/chat/completions", nil, nil); w.Code != 401 {
		t.Fatalf("SSO implicitly granted proxy: %d", w.Code)
	}
	for name, change := range map[string]func(map[string]string){
		"untrusted-peer":   func(h map[string]string) { h["RemoteAddr"] = "198.51.100.2:3000"; h["X-Forwarded-For"] = "10.0.0.2" },
		"bad-secret":       func(h map[string]string) { h[middleware.IdentitySecretHeader] = "wrong" },
		"missing-subject":  func(h map[string]string) { delete(h, middleware.IdentitySubjectHeader) },
		"unmapped-group":   func(h map[string]string) { h[middleware.IdentityGroupsHeader] = "unknown" },
		"expired":          func(h map[string]string) { h[middleware.IdentityExpiresHeader] = "1" },
		"malformed-expiry": func(h map[string]string) { h[middleware.IdentityExpiresHeader] = "NaN" },
		"too-long": func(h map[string]string) {
			h[middleware.IdentityExpiresHeader] = strconv.FormatInt(time.Now().Add(31*24*time.Hour).Unix(), 10)
		},
		"bad-explicit-key": func(h map[string]string) { h["x-api-key"] = "wrong" },
	} {
		t.Run(name, func(t *testing.T) {
			if got := invoke("GET", "/api/access/me", nil, change); got.Code != 401 {
				t.Fatalf("untrusted assertion accepted: %d %s", got.Code, got.Body.String())
			}
		})
	}
	for _, change := range []func(map[string]string){func(h map[string]string) { delete(h, "Origin") }, func(h map[string]string) { delete(h, middleware.IdentityManagementHeader) }} {
		if got := invoke("PUT", "/api/access/channels/ch_owned/state", nil, change); got.Code != 401 {
			t.Fatalf("SSO CSRF write accepted: %d", got.Code)
		}
	}
	w = invoke("POST", "/api/access/session/revoke", nil, nil)
	if w.Code != 200 {
		t.Fatalf("self revoke %d %s", w.Code, w.Body.String())
	}
	if got := invoke("GET", "/api/access/me", nil, nil); got.Code != 401 {
		t.Fatalf("revoked SSO session accepted: %d", got.Code)
	}
	// Updating role mappings cannot silently clear persisted revocation tombstones.
	if err := a.cfgManager.SetIdentityProxyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if got := invoke("GET", "/api/access/me", nil, nil); got.Code != 401 {
		t.Fatal("policy edit revived revoked session")
	}
	if got := invoke("GET", "/api/access/me", nil, func(h map[string]string) { h[middleware.IdentitySessionHeader] = "fresh-session" }); got.Code != 200 {
		t.Fatalf("fresh SSO session failed: %d", got.Code)
	}
}
