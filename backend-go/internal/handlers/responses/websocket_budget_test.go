package responses

import (
	"context"
	"github.com/gorilla/websocket"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/entrybudget"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/session"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebSocketBudgetChecksEachCreateAndIgnoresControls(t *testing.T) {
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_budget\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
	}))
	defer provider.Close()
	price := 1.0
	cm := setupResponsesTestConfigManager(t, []config.UpstreamConfig{{BaseURL: provider.URL, APIKeys: []string{"fake-key"}, ServiceType: "responses", Status: "active", ModelCapabilities: map[string]config.UpstreamModelCapability{"m": {MaxOutputTokens: 10, Pricing: &config.ModelPricing{InputCacheMissPrice: &price, InputCacheHitPrice: &price, OutputPrice: &price}}}}})
	if err := cm.PutEntryKey(config.EntryKey{ID: "ws-budget", KeyHash: config.EntryKeyHash("secret-key"), PrincipalID: "ws-team"}, ""); err != nil {
		t.Fatal(err)
	}
	policy := entrybudget.Policy{PrincipalID: "ws-team", Period: "lifetime", LimitNanoUSD: 1, RequestCapNanoUSD: 1000000000}
	if err := cm.PutPrincipalBudget(policy); err != nil {
		t.Fatal(err)
	}
	manager := entrybudget.NewManager(filepath.Join(t.TempDir(), "ws.db"))
	defer manager.Close()
	env := &config.EnvConfig{ProxyAccessKey: "secret-key", ManagedProxyKeyLookup: cm.FindEntryKey, MaxRequestBodySize: 1 << 20, BudgetManager: manager}
	sched := scheduler.NewChannelScheduler(cm, metrics.NewMetricsManager(), metrics.NewMetricsManager(), metrics.NewMetricsManager(), metrics.NewMetricsManager(), metrics.NewMetricsManager(), session.NewTraceAffinityManager(), nil)
	router := httpbridge.NewApp()
	router.Get("/v1/responses", WebSocketHandler(env, cm, session.NewSessionManager(time.Hour, 100, 100000), sched))
	proxy := httptest.NewServer(httpbridge.Handler(router))
	defer proxy.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/v1/responses", http.Header{"x-api-key": []string{"secret-key"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	create := map[string]interface{}{"type": "response.create", "model": "m", "input": "hello", "max_output_tokens": 10}
	if err := conn.WriteJSON(create); err != nil {
		t.Fatal(err)
	}
	for {
		event := readWebSocketJSON(t, conn)
		if event["type"] == "error" && event["status_code"] == float64(429) {
			break
		}
	}
	if calls.Load() != 0 {
		t.Fatal("WS rejected create contacted upstream")
	}
	policy.LimitNanoUSD = 1000000000
	if err := cm.PutPrincipalBudget(policy); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(create); err != nil {
		t.Fatal(err)
	}
	readWebSocketJSONUntilType(t, conn, "response.completed")
	store, _ := manager.Store()
	deadline := time.Now().Add(time.Second)
	for {
		state, e := store.Snapshot(context.Background(), policy, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		if state.Spent == 2000 && state.Reserved == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("WS usage not settled %+v", state)
		}
		time.Sleep(time.Millisecond)
	}
	before, err := store.Requests(context.Background(), policy.PrincipalID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]interface{}{"type": "response.cancel"}); err != nil {
		t.Fatal(err)
	}
	policy.LimitNanoUSD = 1
	if err := cm.PutPrincipalBudget(policy); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(create); err != nil {
		t.Fatal(err)
	}
	for {
		event := readWebSocketJSON(t, conn)
		if event["type"] == "error" && event["status_code"] == float64(429) {
			break
		}
	}
	after, err := store.Requests(context.Background(), policy.PrincipalID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)+1 || calls.Load() != 1 {
		t.Fatalf("WS control reserved or next turn bypassed: before=%d after=%d calls=%d", len(before), len(after), calls.Load())
	}
}
