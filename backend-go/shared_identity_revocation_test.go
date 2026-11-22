package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/identitysession"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/redisconn"
)

const sharedIdentitySecret = "synthetic-shared-identity-proxy-secret-1234567890"

func sharedIdentityNode(t *testing.T, url, namespace string, preRevoked string) *application {
	t.Helper()
	a := newRouteTestApplication(t, true, "production")
	a.envCfg.IdentityProxySecret = sharedIdentitySecret
	cfg := config.IdentityProxyConfig{Enabled: true, Issuer: "team-identity", RevocationMode: "shared", AllowedOrigins: []string{"https://console.example"}, TrustedCIDRs: []string{"192.0.2.0/24"}, GroupMappings: []config.IdentityGroupMapping{{Group: "team", Role: "viewer"}}}
	if url == "" || strings.Contains(url, "169.254.169.254") {
		cfg.RevocationEpoch = strings.Repeat("a", 64)
	}
	if err := a.cfgManager.SetIdentityProxyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if preRevoked != "" {
		if err := a.cfgManager.RevokeIdentitySession(config.IdentitySessionHash(cfg.Issuer, preRevoked)); err != nil {
			t.Fatal(err)
		}
	}
	if url != "" {
		store, err := identitysession.New(url, namespace)
		if err != nil {
			t.Fatal(err)
		}
		a.envCfg.IdentityProxyRevocations = store
		a.envCfg.IdentityProxyRevocationRedisURL = url
		t.Cleanup(func() { _ = store.Close() })
	}
	if url != "" && !strings.Contains(url, "169.254.169.254") {
		cfg.RevocationMode = "local"
		if err := a.cfgManager.SetIdentityProxyConfig(cfg); err != nil {
			t.Fatal(err)
		}
		cfg.RevocationMode = "shared"
		payload, _ := json.Marshal(cfg)
		response := sharedIdentityCall(httpbridge.Handler(a.newRouter()), "PUT", "/api/identity-proxy", "route-admin", "", string(payload))
		if response.Code != 200 {
			t.Fatalf("activate shared %d %s", response.Code, response.Body.String())
		}
	}
	initializeIdentityRevocationSync(a.envCfg, a.cfgManager)
	return a
}
func sharedIdentityCall(handler http.Handler, method, path, key, sid, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = "192.0.2.7:5432"
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("x-api-key", key)
	}
	if sid != "" {
		request.Header.Set(middleware.IdentitySecretHeader, sharedIdentitySecret)
		request.Header.Set(middleware.IdentitySubjectHeader, "viewer-subject")
		request.Header.Set(middleware.IdentityGroupsHeader, "team")
		request.Header.Set(middleware.IdentitySessionHeader, sid)
		request.Header.Set(middleware.IdentityExpiresHeader, strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
		request.Header.Set(middleware.IdentityManagementHeader, "1")
		request.Header.Set("Origin", "https://console.example")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func TestSharedIdentityRevocationIsImmediateAcrossIndependentHTTPNodes(t *testing.T) {
	url, _ := newIsolatedRedisEndpoint(t)
	first := sharedIdentityNode(t, url, "shared-identity", "legacy-revoked")
	second := sharedIdentityNode(t, url, "shared-identity", "")
	a, b := httpbridge.Handler(first.newRouter()), httpbridge.Handler(second.newRouter())
	for _, handler := range []http.Handler{a, b} {
		if response := sharedIdentityCall(handler, "GET", "/api/access/me", "", "session-a", ""); response.Code != 200 || !strings.Contains(response.Body.String(), `"revocationScope":"shared"`) {
			t.Fatalf("shared identity %d %s", response.Code, response.Body.String())
		}
	}
	if response := sharedIdentityCall(b, "GET", "/api/access/me", "", "legacy-revoked", ""); response.Code != 401 {
		t.Fatalf("local tombstone was not published to shared backend %d %s", response.Code, response.Body.String())
	}
	// A self endpoint ignores arbitrary target IDs supplied in its body.
	response := sharedIdentityCall(a, "POST", "/api/access/session/revoke", "", "session-a", `{"sessionFingerprint":"another-target"}`)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"revocationScope":"shared"`) {
		t.Fatalf("shared logout %d %s", response.Code, response.Body.String())
	}
	if response := sharedIdentityCall(b, "GET", "/api/access/me", "", "session-a", ""); response.Code != 401 {
		t.Fatalf("node B accepted revoked session %d %s", response.Code, response.Body.String())
	}
	if response := sharedIdentityCall(b, "GET", "/api/access/me", "", "session-b", ""); response.Code != 200 {
		t.Fatalf("unrelated session revoked %d %s", response.Code, response.Body.String())
	}
	// Older management clients omit the field. The shared mode and tombstone
	// must survive their ordinary issuer/group policy save on a different node.
	cfg := second.cfgManager.IdentityProxySnapshot()
	cfg.RevocationMode = ""
	cfg.RevocationEpoch = ""
	payload, _ := json.Marshal(cfg)
	if response := sharedIdentityCall(b, "PUT", "/api/identity-proxy", "route-admin", "", string(payload)); response.Code != 200 {
		t.Fatalf("legacy save %d %s", response.Code, response.Body.String())
	}
	if got := second.cfgManager.IdentityProxySnapshot().EffectiveRevocationMode(); got != "shared" {
		t.Fatalf("legacy client downgraded scope %q", got)
	}
	if response := sharedIdentityCall(b, "GET", "/api/access/me", "", "session-a", ""); response.Code != 401 {
		t.Fatalf("legacy save revived shared revocation %d", response.Code)
	}
	// Permanent administrator revocations cannot be shortened by later finite
	// retries, including the aliases accepted by the fingerprint input format.
	fingerprint := config.IdentitySessionHash("team-identity", "session-b")
	for _, expires := range []int64{0, time.Now().Add(time.Hour).Unix()} {
		payload, _ := json.Marshal(map[string]interface{}{"sessionFingerprint": strings.ToUpper(fingerprint), "expiresAt": expires})
		if response := sharedIdentityCall(a, "POST", "/api/identity-proxy/sessions/revoke", "route-admin", "", string(payload)); response.Code != 200 {
			t.Fatalf("admin revoke %d %s", response.Code, response.Body.String())
		}
	}
	client, err := redisconn.New(url)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ttl, err := client.PTTL(context.Background(), "shared-identity:identity:revoked:"+fingerprint).Result()
	if err != nil || ttl != -time.Nanosecond {
		t.Fatalf("permanent revoke was shortened: %v %v", ttl, err)
	}
	if response := sharedIdentityCall(b, "GET", "/api/access/me", "", "session-b", ""); response.Code != 401 {
		t.Fatalf("admin revocation not shared %d", response.Code)
	}
}
func TestSharedIdentityBackendUnavailableCannotFallBackToLocal(t *testing.T) {
	url, stopRedis := newIsolatedRedisEndpoint(t)
	node := sharedIdentityNode(t, url, "shared-failure", "")
	handler := httpbridge.Handler(node.newRouter())
	if response := sharedIdentityCall(handler, "GET", "/api/access/me", "", "live-session", ""); response.Code != 200 {
		t.Fatalf("initial %d %s", response.Code, response.Body.String())
	}
	stopRedis()
	if response := sharedIdentityCall(handler, "GET", "/api/access/me", "", "live-session", ""); response.Code != 503 {
		t.Fatalf("backend failure granted/fell back %d %s", response.Code, response.Body.String())
	}
	if response := sharedIdentityCall(handler, "GET", "/api/identity-proxy", "route-admin", "", ""); response.Code != 200 {
		t.Fatalf("explicit owner recovery key failed %d", response.Code)
	}
	cfg := node.cfgManager.IdentityProxySnapshot()
	cfg.RevocationMode = ""
	cfg.RevocationEpoch = ""
	payload, _ := json.Marshal(cfg)
	if response := sharedIdentityCall(handler, "PUT", "/api/identity-proxy", "route-admin", "", string(payload)); response.Code != 503 {
		t.Fatalf("legacy save during failure did not fail closed %d", response.Code)
	}
	if node.cfgManager.IdentityProxySnapshot().EffectiveRevocationMode() != "shared" {
		t.Fatal("backend outage silently changed scope")
	}
	// An explicit global-administrator choice may return to local semantics.
	cfg.RevocationMode = "local"
	payload, _ = json.Marshal(cfg)
	if response := sharedIdentityCall(handler, "PUT", "/api/identity-proxy", "route-admin", "", string(payload)); response.Code != 200 {
		t.Fatalf("explicit local %d %s", response.Code, response.Body.String())
	}
	if response := sharedIdentityCall(handler, "GET", "/api/access/me", "", "live-session", ""); response.Code != 200 || !strings.Contains(response.Body.String(), `"revocationScope":"local"`) {
		t.Fatalf("local semantics not explicit %d %s", response.Code, response.Body.String())
	}
}
func TestSharedIdentityRequiresBackendAndHonorsGlobalOutboundPolicy(t *testing.T) {
	for _, url := range []string{"", "redis://user:synthetic-secret@169.254.169.254:6379"} {
		node := sharedIdentityNode(t, url, "required", "")
		handler := httpbridge.Handler(node.newRouter())
		response := sharedIdentityCall(handler, "GET", "/api/access/me", "", "session", "")
		if response.Code != 503 || strings.Contains(response.Body.String(), "synthetic-secret") {
			t.Fatalf("unsafe shared mode %d %s", response.Code, response.Body.String())
		}
	}
}

func TestSharedIdentityStateLossFailsClosedAcrossRestartAndModeChanges(t *testing.T) {
	endpoint, _ := newIsolatedRedisEndpoint(t)
	first := sharedIdentityNode(t, endpoint, "epoch-recovery", "")
	second := sharedIdentityNode(t, endpoint, "epoch-recovery", "")
	a, b := httpbridge.Handler(first.newRouter()), httpbridge.Handler(second.newRouter())
	if r := sharedIdentityCall(a, "POST", "/api/access/session/revoke", "", "revoked", ""); r.Code != 200 {
		t.Fatalf("revoke %d", r.Code)
	}
	saved := second.cfgManager.IdentityProxySnapshot()
	if saved.RevocationEpoch == "" || first.cfgManager.IdentityProxySnapshot().RevocationEpoch != saved.RevocationEpoch {
		t.Fatal("nodes did not persist same epoch")
	}
	unbound := newRouteTestApplication(t, true, "production")
	unbound.envCfg.IdentityProxySecret = sharedIdentitySecret
	unboundStore, err := identitysession.New(endpoint, "epoch-recovery")
	if err != nil {
		t.Fatal(err)
	}
	defer unboundStore.Close()
	unbound.envCfg.IdentityProxyRevocations = unboundStore
	unboundCfg := saved
	unboundCfg.RevocationEpoch = ""
	if err := unbound.cfgManager.SetIdentityProxyConfig(unboundCfg); err != nil {
		t.Fatal(err)
	}
	initializeIdentityRevocationSync(unbound.envCfg, unbound.cfgManager)
	if r := sharedIdentityCall(httpbridge.Handler(unbound.newRouter()), "GET", "/api/access/me", "", "live", ""); r.Code != 503 {
		t.Fatalf("unbound config joined existing realm %d", r.Code)
	}
	client, err := redisconn.New(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx := context.Background()
	keys, err := client.Keys(ctx, "epoch-recovery:identity:*").Result()
	if err != nil {
		t.Fatal(err)
	}
	type record struct {
		key, dump string
		ttl       time.Duration
	}
	var snapshot []record
	for _, key := range keys {
		dump, err := client.Dump(ctx, key).Result()
		if err != nil {
			t.Fatal(err)
		}
		ttl, err := client.PTTL(ctx, key).Result()
		if err != nil {
			t.Fatal(err)
		}
		if ttl < 0 {
			ttl = 0
		}
		snapshot = append(snapshot, record{key, dump, ttl})
	}
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	for _, h := range []http.Handler{a, b} {
		if r := sharedIdentityCall(h, "GET", "/api/access/me", "", "live", ""); r.Code != 503 {
			t.Fatalf("lost realm allowed auth %d", r.Code)
		}
	}
	legacy := saved
	legacy.RevocationMode = ""
	legacy.RevocationEpoch = ""
	put := func(h http.Handler, cfg config.IdentityProxyConfig) int {
		data, _ := json.Marshal(cfg)
		return sharedIdentityCall(h, "PUT", "/api/identity-proxy", "route-admin", "", string(data)).Code
	}
	if got := put(b, legacy); got != 503 {
		t.Fatalf("legacy save recreated realm %d", got)
	}
	legacy.RevocationMode = "local"
	if got := put(a, legacy); got != 200 {
		t.Fatalf("explicit local %d", got)
	}
	if first.cfgManager.IdentityProxySnapshot().RevocationEpoch != saved.RevocationEpoch {
		t.Fatal("local mode erased epoch")
	}
	legacy.RevocationMode = "shared"
	if got := put(a, legacy); got != 503 {
		t.Fatalf("local/shared recreated lost realm %d", got)
	}
	// A fresh process uses only durable configuration and cannot adopt/rebuild a
	// missing namespace, even though its per-process synchronization cache is empty.
	fresh := newRouteTestApplication(t, true, "production")
	fresh.envCfg.IdentityProxySecret = sharedIdentitySecret
	store, err := identitysession.New(endpoint, "epoch-recovery")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	fresh.envCfg.IdentityProxyRevocations = store
	if err := fresh.cfgManager.SetIdentityProxyConfig(saved); err != nil {
		t.Fatal(err)
	}
	initializeIdentityRevocationSync(fresh.envCfg, fresh.cfgManager)
	c := httpbridge.Handler(fresh.newRouter())
	if r := sharedIdentityCall(c, "GET", "/api/access/me", "", "live", ""); r.Code != 503 {
		t.Fatalf("fresh process rebuilt missing state %d", r.Code)
	}
	if count, err := client.DBSize(ctx).Result(); err != nil || count != 0 {
		t.Fatalf("failed requests created state %d %v", count, err)
	}
	// Recovery restores the entire snapshot, never just an empty epoch marker.
	for _, item := range snapshot {
		if err := client.RestoreReplace(ctx, item.key, item.ttl, item.dump).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if got := put(a, legacy); got != 200 {
		t.Fatalf("restored realm unavailable %d", got)
	}
	for _, h := range []http.Handler{a, b, c} {
		if r := sharedIdentityCall(h, "GET", "/api/access/me", "", "live", ""); r.Code != 200 {
			t.Fatalf("restored live session %d %s", r.Code, r.Body.String())
		}
		if r := sharedIdentityCall(h, "GET", "/api/access/me", "", "revoked", ""); r.Code != 401 {
			t.Fatalf("restored revoked session revived %d", r.Code)
		}
	}
}

func TestSharedIdentityOutboundPolicyAppliesToCachedConnections(t *testing.T) {
	endpoint, _ := newIsolatedRedisEndpoint(t)
	node := sharedIdentityNode(t, endpoint, "egress-policy", "")
	handler := httpbridge.Handler(node.newRouter())
	if r := sharedIdentityCall(handler, "GET", "/api/access/me", "", "live", ""); r.Code != 200 {
		t.Fatalf("initial %d", r.Code)
	}
	t.Setenv("YUNQI_OUTBOUND_ALLOWED_HOSTS", "only-this-host.invalid")
	if r := sharedIdentityCall(handler, "GET", "/api/access/me", "", "live", ""); r.Code != 503 {
		t.Fatalf("pooled Redis bypassed outbound policy %d", r.Code)
	}
}
