package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/entrybudget"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/warmup"
)

func TestPrincipalBudgetRealProxyPreflightAndSettlement(t *testing.T) {
	var calls atomic.Int64
	var mode atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		if mode.Load() == 4 {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if mode.Load() == 1 {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid_request","type":"invalid_request_error"}}`))
			return
		}
		usage := `"prompt_tokens":2,"completion_tokens":1`
		if mode.Load() == 2 || mode.Load() == 6 {
			usage = `"prompt_tokens":0,"completion_tokens":0`
		}
		var output io.Writer = w
		if mode.Load() == 5 || mode.Load() == 6 {
			w.Header().Set("Content-Encoding", "gzip")
			encoded := gzip.NewWriter(w)
			defer encoded.Close()
			output = encoded
		}
		_, _ = output.Write([]byte(`{"id":"chat","object":"chat.completion","model":"budget-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{` + usage + `}}`))
	}))
	defer upstream.Close()
	a := newRouteTestApplication(t, true, "production")
	a.envCfg.RequestTimeout = 10000
	a.envCfg.BudgetManager = entrybudget.NewManager(filepath.Join(t.TempDir(), "budget.db"))
	defer a.envCfg.BudgetManager.Close()
	a.channelScheduler = scheduler.NewChannelScheduler(a.cfgManager, a.messagesMetricsManager, a.responsesMetricsManager, a.geminiMetricsManager, a.chatMetricsManager, a.imagesMetricsManager, session.NewTraceAffinityManager(), warmup.NewURLManager(time.Second, 1), a.vectorsMetricsManager)
	price := 1.0
	if err := a.cfgManager.AddChatUpstream(config.UpstreamConfig{ChannelUID: "budget_ch", Name: "budget", BaseURL: upstream.URL, APIKeys: []string{"fake-provider"}, ServiceType: "openai", SupportedModels: []string{"budget-model"}, Status: "active", ModelCapabilities: map[string]config.UpstreamModelCapability{"budget-model": {MaxOutputTokens: 100, Pricing: &config.ModelPricing{InputCacheMissPrice: &price, OutputPrice: &price, InputCacheHitPrice: &price}}}}); err != nil {
		t.Fatal(err)
	}
	if err := a.cfgManager.PutEntryKey(config.EntryKey{ID: "budget_key", KeyHash: config.EntryKeyHash("route-proxy"), PrincipalID: "budget-team"}, ""); err != nil {
		t.Fatal(err)
	}
	policy := entrybudget.Policy{PrincipalID: "budget-team", Period: "month", Timezone: "UTC", LimitNanoUSD: 1, RequestCapNanoUSD: 1_000_000_000}
	if err := a.cfgManager.PutPrincipalBudget(policy); err != nil {
		t.Fatal(err)
	}
	handler := httpbridge.Handler(a.newRouter())
	invoke := func(method, path, key, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("x-api-key", key)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body := `{"model":"budget-model","max_tokens":10,"messages":[{"role":"user","content":"hello"}]}`
	if w := invoke("POST", "/v1/chat/completions", "route-proxy", body); w.Code != 429 {
		t.Fatalf("budget reject %d %s", w.Code, w.Body.String())
	}
	if calls.Load() != 0 {
		t.Fatal("budget rejection contacted provider")
	}
	policy.LimitNanoUSD = 1_000_000_000
	if w := invoke("PUT", "/api/budgets", "route-admin", `{"principalId":"budget-team","period":"month","timezone":"UTC","limitUsd":"1","requestCapUsd":"1"}`); w.Code != 200 {
		t.Fatalf("admin update %d %s", w.Code, w.Body.String())
	}
	if err := a.cfgManager.PutAdminCredential(config.AdminCredential{ID: "budget-viewer", KeyHash: config.EntryKeyHash("budget-viewer-key"), ActorID: "budget-auditor", Role: "viewer"}, ""); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PUT"} {
		if w := invoke(method, "/api/budgets", "budget-viewer-key", `{}`); w.Code != 403 {
			t.Fatalf("limited role budgets %s %d", method, w.Code)
		}
	}
	if w := invoke("PUT", "/api/budgets", "route-admin", `{"principalId":"budget-team","limitUsd":1,"requestCapUsd":"1"}`); w.Code != 400 {
		t.Fatalf("coerced numeric money %d", w.Code)
	}
	success := invoke("POST", "/v1/chat/completions", "route-proxy", body)
	if success.Code != 200 {
		t.Fatalf("proxy %d %s", success.Code, success.Body.String())
	}
	diagnosticID := success.Header().Get("X-Yunqi-Request-Id")
	if diagnosticID == "" {
		t.Fatal("missing server diagnostic ID")
	}
	store, err := a.envCfg.BudgetManager.Store()
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.Snapshot(context.Background(), policy, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if state.Reserved != 0 || state.Spent != 3000 {
		t.Fatalf("usage settlement %+v", state)
	}
	// Provider zero is authoritative and releases the entire input/output hold.
	mode.Store(2)
	if w := invoke("POST", "/v1/chat/completions", "route-proxy", body); w.Code != 200 {
		t.Fatalf("zero %d %s", w.Code, w.Body.String())
	}
	state, err = store.Snapshot(context.Background(), policy, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if state.Spent != 3000 || state.Reserved != 0 {
		t.Fatalf("zero usage retained estimate %+v", state)
	}
	mode.Store(1)
	if w := invoke("POST", "/v1/chat/completions", "route-proxy", body); w.Code != 400 {
		t.Fatalf("upstream rejection %d %s", w.Code, w.Body.String())
	}
	state, err = store.Snapshot(context.Background(), policy, time.Now())
	if err != nil || state.Spent != 3000 || state.Reserved != 0 {
		t.Fatalf("failed upstream retained hold %+v %v", state, err)
	}
	for _, bad := range []string{
		strings.Replace(body, `"max_tokens":10`, `"max_tokens":10,"tools":[{"type":"web_search_preview"}]`, 1), strings.Replace(body, `"max_tokens":10`, `"max_tokens":"10"`, 1), strings.Replace(body, `"content":"hello"`, `"content":[{"type":"image_url","image_url":{"url":"private"}}]`, 1)} {
		if w := invoke("POST", "/v1/chat/completions", "route-proxy", bad); w.Code != 400 {
			t.Fatalf("incomplete quote %d %s", w.Code, w.Body.String())
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("pricing rejection called provider %d", calls.Load())
	}
	w := invoke("GET", "/v1/budget?principalId=another-team", "route-proxy", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "list_price_usd") || !strings.Contains(w.Body.String(), "budget-team") || strings.Contains(w.Body.String(), "another-team") {
		t.Fatalf("own budget %d %s", w.Code, w.Body.String())
	}
	if w := invoke("GET", "/v1/budget", "wrong", ""); w.Code != 401 {
		t.Fatalf("budget auth %d", w.Code)
	}
	records, err := store.Requests(context.Background(), "budget-team", 100)
	if err != nil {
		t.Fatal(err)
	}
	var provider int
	for _, record := range records {
		if record.ChargedNanoUSD > 0 {
			attempts, err := store.Attempts(context.Background(), record.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(attempts) != 1 || attempts[0].PricingVersion == "" || !json.Valid(attempts[0].Pricing) || attempts[0].UsageSource != "provider" || !json.Valid(attempts[0].BillingUsage) || record.ID != diagnosticID {
				t.Fatalf("missing immutable price audit %+v", attempts)
			}
			provider++
		}
	}
	if provider != 1 {
		t.Fatalf("non-idempotent settlement %d", provider)
	}
	if w := invoke("GET", "/api/budgets/budget-team/requests/"+diagnosticID, "route-admin", ""); w.Code != 200 {
		t.Fatalf("own audit %d %s", w.Code, w.Body.String())
	}
	other := policy
	other.PrincipalID = "other-team"
	if err := a.cfgManager.PutPrincipalBudget(other); err != nil {
		t.Fatal(err)
	}
	if w := invoke("GET", "/api/budgets/other-team/requests/"+diagnosticID, "route-admin", ""); w.Code != 404 {
		t.Fatalf("cross owner audit %d %s", w.Code, w.Body.String())
	}
	// Explicit compressed provider responses must retain exact known and zero
	// accounting despite the protocol handler also normalizing decompression.
	for _, compressedMode := range []int64{5, 6} {
		mode.Store(compressedMode)
		if w := invoke("POST", "/v1/chat/completions", "route-proxy", body); w.Code != 200 {
			t.Fatalf("gzip mode %d: %d %s", compressedMode, w.Code, w.Body.String())
		}
	}
	state, err = store.Snapshot(context.Background(), policy, time.Now())
	if err != nil || state.Spent != 6000 || state.Reserved != 0 {
		t.Fatalf("gzip known/zero not exact %+v %v", state, err)
	}
	// A gateway-owned duration lease cancels actual supplier work, retains an
	// explicit conservative charge, and never damages provider key health.
	policy.MaxRequestSeconds = 1
	if err := a.cfgManager.PutPrincipalBudget(policy); err != nil {
		t.Fatal(err)
	}
	mode.Store(4)
	if w := invoke("POST", "/v1/chat/completions", "route-proxy", body); w.Code != 503 {
		t.Fatalf("duration lease %d %s", w.Code, w.Body.String())
	}
	mode.Store(0)
	if w := invoke("POST", "/v1/chat/completions", "route-proxy", body); w.Code != 200 {
		t.Fatalf("budget timeout damaged healthy key %d %s", w.Code, w.Body.String())
	}

}
