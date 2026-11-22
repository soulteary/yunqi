package handlers

import (
	"database/sql"
	"encoding/json"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPrincipalCostReportAndHistoryUseSameOpaqueScope(t *testing.T) {
	store, err := metrics.NewSQLiteStore(&metrics.SQLiteStoreConfig{DBPath: filepath.Join(t.TempDir(), "cost.db"), RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager := metrics.NewMetricsManagerWithPersistence(100, 0.5, store, "messages")
	defer manager.Stop()
	first, other := metrics.PrincipalAttributionID("principal:private-team@example.invalid"), metrics.PrincipalAttributionID("principal:other")
	now := time.Now().Truncate(time.Second)
	for _, record := range []metrics.PersistentRecord{{Timestamp: now, APIType: "messages", Model: "model-a", PrincipalID: first, ProxyKeyMask: "old-key", ListCostUSD: 0.2}, {Timestamp: now, APIType: "messages", Model: "model-b", PrincipalID: first, ProxyKeyMask: "new-key", ListCostUSD: 0.3}, {Timestamp: now, APIType: "messages", Model: "model-a", PrincipalID: other, ProxyKeyMask: "old-key", ListCostUSD: 9}, {Timestamp: now, APIType: "messages", Model: "model-a", ProxyKeyMask: "old-key", ListCostUSD: 20}} {
		store.AddRecord(record)
	}
	_, app := httpbridge.TestContext(httptest.NewRecorder())
	app.Get("/cost", GetCostReport(&CostReportDeps{MetricsManagers: map[string]*metrics.MetricsManager{"messages": manager}}))
	app.Get("/history", GetRequestHistory(store))
	call := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	response := call("/cost?groupBy=principal")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var grouped struct {
		Rows []costReportRow `json:"rows"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &grouped); err != nil {
		t.Fatal(err)
	}
	if len(grouped.Rows) != 3 || strings.Contains(response.Body.String(), "private-team") || strings.Contains(response.Body.String(), "@example.invalid") {
		t.Fatalf("unsafe or merged principals=%s", response.Body.String())
	}
	response = call("/cost?groupBy=model&principalId=" + url.QueryEscape(first))
	var scoped struct {
		Rows        []costReportRow `json:"rows"`
		PrincipalID string          `json:"principalId"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &scoped) != nil || scoped.PrincipalID != first || len(scoped.Rows) != 2 {
		t.Fatalf("scope=%s", response.Body.String())
	}
	var total float64
	for _, row := range scoped.Rows {
		total += row.ListCostUSD
	}
	if total != 0.5 {
		t.Fatalf("cost leak=%f", total)
	}
	response = call("/history?type=messages&since=0&until=4102444800&principalId=" + first)
	var page metrics.RequestHistoryPage
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil || len(page.Records) != 2 {
		t.Fatalf("history=%d %s", response.Code, response.Body.String())
	}
	for _, record := range page.Records {
		if record.PrincipalID != first {
			t.Fatal("history escaped principal scope")
		}
	}
	for _, path := range []string{"/cost?principalId=raw-user@example.invalid", "/history?type=messages&since=0&until=4102444800&principalId=raw-user@example.invalid"} {
		if got := call(path); got.Code != 400 {
			t.Fatalf("raw identity filter accepted: %d %s", got.Code, got.Body.String())
		}
	}
}

type unscopedPrincipalCostStore struct {
	testCostReportStore
	calls int
}

func (s *unscopedPrincipalCostStore) QueryCostReport(apiType string, since time.Time, groupBy string) ([]metrics.CostReportRow, error) {
	s.calls++
	return []metrics.CostReportRow{{GroupKey: "unfiltered-data", TotalRequests: 99}}, nil
}
func TestPrincipalCostUnsupportedStoreCannotFallBackToUnfilteredData(t *testing.T) {
	store := &unscopedPrincipalCostStore{}
	manager := metrics.NewMetricsManagerWithPersistence(10, 0.5, store, "messages")
	defer manager.Stop()
	app := httpbridge.NewApp()
	app.Get("/cost", GetCostReport(&CostReportDeps{MetricsManagers: map[string]*metrics.MetricsManager{"messages": manager}}))
	for _, path := range []string{"/cost?groupBy=principal", "/cost?groupBy=model&principalId=" + metrics.PrincipalAttributionID("principal:team")} {
		response := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != 400 || strings.Contains(response.Body.String(), "unfiltered-data") {
			t.Fatalf("unsupported store silently lost scope: %d %s", response.Code, response.Body.String())
		}
	}
	if store.calls != 0 {
		t.Fatal("unscoped backend was queried before rejecting principal scope")
	}
}

func TestPrincipalOverviewAndUnknownDrilldownDoNotInventFreeMissingModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-cost.db")
	store, err := metrics.NewSQLiteStore(&metrics.SQLiteStoreConfig{DBPath: path, RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err = raw.Exec(`INSERT INTO request_records(metrics_key,base_url,key_mask,timestamp,success,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,api_type,model,proxy_key_mask,list_cost_usd) VALUES('legacy','https://fake.invalid','mask',?,1,8,0,0,0,'messages','','old-mask',NULL)`, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	manager := metrics.NewMetricsManagerWithPersistence(100, 0.5, store, "messages")
	defer manager.Stop()
	app := httpbridge.NewApp()
	app.Get("/cost", GetCostReport(&CostReportDeps{MetricsManagers: map[string]*metrics.MetricsManager{"messages": manager}}))
	for _, path := range []string{"/cost?groupBy=principal", "/cost?groupBy=principal&principalId=unknown", "/cost?groupBy=model&principalId=unknown"} {
		response := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		var result struct {
			Rows []costReportRow `json:"rows"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Rows) != 1 {
			t.Fatalf("missing-model row dropped: %d %s", response.Code, response.Body.String())
		}
		row := result.Rows[0]
		if row.TotalRequests != 1 || row.InputTokens != 8 || row.PricingComplete || row.ListCostUSD != 0 {
			t.Fatalf("missing pricing became complete free usage: %+v", row)
		}
	}
}

func TestPrincipalCostReportUsesOneRealUpperBoundForSummaryBreakdownAndHistory(t *testing.T) {
	store, err := metrics.NewSQLiteStore(&metrics.SQLiteStoreConfig{DBPath: filepath.Join(t.TempDir(), "cost.db"), RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager := metrics.NewMetricsManagerWithPersistence(100, 0.5, store, "messages")
	defer manager.Stop()
	identity := metrics.PrincipalAttributionID("principal:team")
	for _, record := range []metrics.PersistentRecord{{APIType: "messages", Timestamp: time.Now().Add(-time.Minute), Model: "current-model", PrincipalID: identity, ListCostUSD: 1}, {APIType: "messages", Timestamp: time.Now().Add(time.Hour), Model: "future-unpriced-model", PrincipalID: identity, ListCostUSD: 99}} {
		store.AddRecord(record)
	}
	app := httpbridge.NewApp()
	app.Get("/cost", GetCostReport(&CostReportDeps{MetricsManagers: map[string]*metrics.MetricsManager{"messages": manager}}))
	app.Get("/history", GetRequestHistory(store))
	call := func(path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response
	}
	for _, path := range []string{"/cost?groupBy=principal", "/cost?groupBy=principal&principalId=" + identity} {
		response := call(path)
		var result struct {
			Since int64           `json:"since"`
			Until int64           `json:"until"`
			Rows  []costReportRow `json:"rows"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Rows) != 1 || result.Rows[0].TotalRequests != 1 || result.Rows[0].ListCostUSD != 1 {
			t.Fatalf("future data escaped claimed interval: %d %s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "future-unpriced-model") {
			t.Fatal("model breakdown did not use summary upper bound")
		}
		history := call("/history?type=messages&principalId=" + identity + "&since=" + strconv.FormatInt(result.Since, 10) + "&until=" + strconv.FormatInt(result.Until, 10))
		var page metrics.RequestHistoryPage
		if history.Code != 200 || json.Unmarshal(history.Body.Bytes(), &page) != nil || len(page.Records) != 1 || page.Records[0].Model != "current-model" {
			t.Fatalf("report/history windows diverged: %d %s", history.Code, history.Body.String())
		}
	}
}
