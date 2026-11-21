package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/warmup"
)

func TestManagedEntryKeyGuardsRealProxyAndRotation(t *testing.T) {
	var allowedCalls, deniedCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/denied/") {
			deniedCalls.Add(1)
		} else {
			allowedCalls.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chat","object":"chat.completion","model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`))
	}))
	defer upstream.Close()
	a := newRouteTestApplication(t, true, "production")
	a.envCfg.RequestTimeout = 10000
	a.channelScheduler = scheduler.NewChannelScheduler(a.cfgManager, a.messagesMetricsManager, a.responsesMetricsManager, a.geminiMetricsManager, a.chatMetricsManager, a.imagesMetricsManager, session.NewTraceAffinityManager(), warmup.NewURLManager(time.Second, 1), a.vectorsMetricsManager)
	for i, item := range []struct{ uid, path string }{{"ch_denied", "/denied"}, {"ch_allowed", "/allowed"}} {
		if err := a.cfgManager.AddChatUpstream(config.UpstreamConfig{ChannelUID: item.uid, Name: item.uid, BaseURL: upstream.URL + item.path, APIKeys: []string{"fake-upstream"}, ServiceType: "openai", SupportedModels: []string{"test-model"}, Status: "active", Priority: i + 1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.cfgManager.AddResponsesUpstream(config.UpstreamConfig{ChannelUID: "ch_compact_denied", Name: "compact-denied", BaseURL: upstream.URL + "/denied", APIKeys: []string{"fake-upstream"}, ServiceType: "responses", SupportedModels: []string{"test-model"}, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	policy := config.EntryKey{ID: "entry", KeyHash: config.EntryKeyHash("route-proxy"), PrincipalID: "team-a", Models: []string{"test-model"}, ChannelUIDs: []string{"ch_allowed"}, CIDRs: []string{"192.0.2.0/24"}}
	if err := a.cfgManager.PutEntryKey(policy, ""); err != nil {
		t.Fatal(err)
	}
	handler := httpbridge.Handler(a.newRouter())
	invoke := func(path, key, body, remote, xff string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", key)
		if remote != "" {
			req.RemoteAddr = remote
		}
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	good := `{"model":"test-model","messages":[{"role":"user","content":"hello"}]}`
	if w := invoke("/v1/chat/completions", "route-proxy", good, "192.0.2.2:3000", ""); w.Code != 200 {
		t.Fatalf("allowed proxy %d %s", w.Code, w.Body.String())
	}
	if deniedCalls.Load() != 0 || allowedCalls.Load() != 1 {
		t.Fatalf("ACL routing calls denied=%d allowed=%d", deniedCalls.Load(), allowedCalls.Load())
	}
	for _, tc := range []struct{ path, body, remote, xff string }{
		{"/v1/quote", `{"channelKind":"chat","model":"forbidden","inputTokens":2,"outputTokens":1}`, "192.0.2.2:3000", ""},
		{"/v1/chat/completions", `{"model":"forbidden","messages":[]}`, "192.0.2.2:3000", ""},
		{"/v1/messages/count_tokens", `{"model":"forbidden","messages":[]}`, "192.0.2.2:3000", ""},
		{"/v1/chat/completions", good, "198.51.100.2:3000", "192.0.2.2"},
		{"/v1/responses/compact", `{"model":"test-model"}`, "192.0.2.2:3000", ""},
	} {
		w := invoke(tc.path, "route-proxy", tc.body, tc.remote, tc.xff)
		if w.Code == 200 {
			t.Fatalf("policy bypass: %s %s", tc.path, w.Body.String())
		}
	}
	quote := invoke("/v1/quote", "route-proxy", `{"channelKind":"chat","model":"test-model","inputTokens":2,"outputTokens":1}`, "192.0.2.2:3000", "")
	if quote.Code != 200 {
		t.Fatalf("scoped quote failed %d %s", quote.Code, quote.Body.String())
	}
	if allowedCalls.Load() != 1 || deniedCalls.Load() != 0 {
		t.Fatal("rejected requests reached upstream")
	}
	w := invoke("/api/entry-keys/entry/rotate", "route-admin", `{}`, "192.0.2.2:3000", "")
	if w.Code != 201 {
		t.Fatalf("rotate %d %s", w.Code, w.Body.String())
	}
	var rotated struct {
		Key      string `json:"key"`
		EntryKey struct {
			PrincipalID string `json:"principalId"`
		} `json:"entryKey"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.EntryKey.PrincipalID != "team-a" || rotated.Key == "" {
		t.Fatalf("rotation lost principal: %s", w.Body.String())
	}
	if w := invoke("/v1/chat/completions", "route-proxy", good, "192.0.2.2:3000", ""); w.Code != 401 {
		t.Fatalf("disabled static key fallback: %d", w.Code)
	}
	if w := invoke("/v1/chat/completions", rotated.Key, good, "192.0.2.2:3000", ""); w.Code != 200 {
		t.Fatalf("new key failed: %d %s", w.Code, w.Body.String())
	}
	for _, key := range a.cfgManager.EntryKeysSnapshot() {
		if key.KeyHash == config.EntryKeyHash(rotated.Key) {
			past := time.Now().Add(-time.Second)
			key.ExpiresAt = &past
			if err := a.cfgManager.PutEntryKey(key, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	if w := invoke("/v1/chat/completions", rotated.Key, good, "192.0.2.2:3000", ""); w.Code != 401 {
		t.Fatalf("expired key: %d", w.Code)
	}
}
