package main

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/warmup"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrincipalCostAcrossEntryKeyRotationAndForgedIdentityHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, redisAdmissionReply)
	}))
	defer upstream.Close()
	app := newRouteTestApplication(t, true, "production")
	app.envCfg.RequestTimeout = 10000
	store, err := metrics.NewSQLiteStore(&metrics.SQLiteStoreConfig{DBPath: filepath.Join(t.TempDir(), "metrics.db"), RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	app.metricsStore = store
	app.chatMetricsManager = metrics.NewMetricsManagerWithPersistence(100, 0.7, store, "chat")
	t.Cleanup(app.chatMetricsManager.Stop)
	app.channelScheduler = scheduler.NewChannelScheduler(app.cfgManager, app.messagesMetricsManager, app.responsesMetricsManager, app.geminiMetricsManager, app.chatMetricsManager, app.imagesMetricsManager, session.NewTraceAffinityManager(), warmup.NewURLManager(time.Second, 1), app.vectorsMetricsManager)
	if err := app.cfgManager.AddChatUpstream(redisAdmissionUpstream(upstream.URL, "openai")); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ id, key, principal string }{{"one", "route-proxy", "team-stable"}, {"two", "route-secondary", "team-stable"}, {"other", "route-other", "other-team"}} {
		if err := app.cfgManager.PutEntryKey(config.EntryKey{ID: entry.id, KeyHash: config.EntryKeyHash(entry.key), PrincipalID: entry.principal}, ""); err != nil {
			t.Fatal(err)
		}
	}
	handler := httpbridge.Handler(app.newRouter())
	call := func(method, path, key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", key)
		req.Header.Set("X-Yunqi-Principal", "principal:team-stable")
		req.Header.Set("X-Principal-ID", "team-stable")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	if w := call("POST", "/v1/chat/completions", "route-proxy", redisAdmissionBody); w.Code != 200 {
		t.Fatalf("initial request=%d %s", w.Code, w.Body.String())
	}
	rotation := call("POST", "/api/entry-keys/one/rotate", "route-admin", `{}`)
	var rotated struct {
		Key string `json:"key"`
	}
	if rotation.Code != 201 || json.Unmarshal(rotation.Body.Bytes(), &rotated) != nil || rotated.Key == "" {
		t.Fatalf("rotation=%d %s", rotation.Code, rotation.Body.String())
	}
	for _, key := range []string{rotated.Key, "route-secondary", "route-other"} {
		if w := call("POST", "/v1/chat/completions", key, redisAdmissionBody); w.Code != 200 {
			t.Fatalf("request=%d %s", w.Code, w.Body.String())
		}
	}
	if w := call("POST", "/v1/chat/completions", "route-proxy", redisAdmissionBody); w.Code != 401 {
		t.Fatalf("revoked key accepted=%d", w.Code)
	}
	store.AddRecord(metrics.PersistentRecord{APIType: "chat", Timestamp: time.Now(), Model: "shared-model", ProxyKeyMask: "entry:one", ListCostUSD: 5})
	response := call("GET", "/api/reports/cost?groupBy=principal&type=chat", "route-admin", "")
	if response.Code != 200 {
		t.Fatalf("report=%d %s", response.Code, response.Body.String())
	}
	var report struct {
		Rows []struct {
			GroupKey      string `json:"groupKey"`
			TotalRequests int64  `json:"totalRequests"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int64{}
	for _, row := range report.Rows {
		counts[row.GroupKey] = row.TotalRequests
	}
	identity := metrics.PrincipalAttributionID("principal:team-stable")
	if counts[identity] != 3 || counts[metrics.PrincipalAttributionID("principal:other-team")] != 1 || counts[metrics.UnknownPrincipal] != 1 {
		t.Fatalf("key rotation/header spoof/legacy attribution=%+v", counts)
	}
	for _, secret := range []string{"team-stable", "other-team", rotated.Key} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("raw identity/credential leaked: %s", secret)
		}
	}
	history := call("GET", "/api/reports/requests?type=chat&since=0&until=4102444800&principalId="+identity, "route-admin", "")
	var page metrics.RequestHistoryPage
	if history.Code != 200 || json.Unmarshal(history.Body.Bytes(), &page) != nil || len(page.Records) != 3 {
		t.Fatalf("history=%d %s", history.Code, history.Body.String())
	}
	for _, entry := range page.Records {
		if entry.PrincipalID != identity {
			t.Fatal("principal history escaped scope")
		}
	}
	if response := call("GET", "/api/reports/cost?groupBy=principal&type=chat", "route-secondary", ""); response.Code != 401 && response.Code != 403 {
		t.Fatalf("data-plane key accessed other principals: %d", response.Code)
	}
}
