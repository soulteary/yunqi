package metrics

import (
	"context"
	"github.com/soulteary/yunqi/internal/types"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrincipalAttributionSeparatesStableIdentityFromCredentials(t *testing.T) {
	stable := PrincipalAttributionID("principal:team-private@example.invalid")
	if !ValidPrincipalAttributionID(stable) || !strings.HasPrefix(stable, "p_") || strings.Contains(stable, "private") || strings.Contains(PrincipalAttributionLabel(stable), "team") {
		t.Fatalf("unsafe attribution=%s", stable)
	}
	if stable != PrincipalAttributionID("principal:team-private@example.invalid") {
		t.Fatal("identity unstable")
	}
	if stable == PrincipalAttributionID("principal:other-team") || stable == PrincipalAttributionID("key:team-private@example.invalid") {
		t.Fatal("different identities collided")
	}
	if !strings.HasPrefix(PrincipalAttributionID("key:opaque-hash"), "k_") {
		t.Fatal("credential identity pretends to be stable principal")
	}
	if PrincipalAttributionID("") != "" || PrincipalAttributionID("spoofed-user-header") != "" {
		t.Fatal("unauthenticated labels accepted")
	}
}
func TestPrincipalCostRotatedKeysAggregateWithoutReassigningLegacyMasks(t *testing.T) {
	s := historyTestStore(t)
	now := time.Now().Truncate(time.Second)
	first, other := PrincipalAttributionID("principal:team-a"), PrincipalAttributionID("principal:team-b")
	records := []PersistentRecord{
		{Timestamp: now, APIType: "chat", Model: "m1", ProxyKeyMask: "old-key", PrincipalID: first, ListCostUSD: 0.1, InputTokens: 1},
		{Timestamp: now, APIType: "chat", Model: "m2", ProxyKeyMask: "new-key", PrincipalID: first, ListCostUSD: 0.2, InputTokens: 2},
		{Timestamp: now, APIType: "chat", Model: "m1", ProxyKeyMask: "old-key", PrincipalID: other, ListCostUSD: 0.4, InputTokens: 4},
		{Timestamp: now, APIType: "chat", Model: "m1", ProxyKeyMask: "old-key", ListCostUSD: 0.8, InputTokens: 8},
	}
	if err := s.batchInsertRecords(records); err != nil {
		t.Fatal(err)
	}
	rows, err := s.QueryCostReport("chat", now.Add(-time.Hour), "principal")
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	sums := map[string]CostReportRow{}
	for _, row := range rows {
		sums[row.GroupKey] = row
	}
	if sums[first].TotalRequests != 2 || math.Abs(sums[first].ListCostUSD-0.3) > 1e-9 || sums[other].TotalRequests != 1 || sums[UnknownPrincipal].ListCostUSD != 0.8 {
		t.Fatalf("wrong identity grouping: %+v", sums)
	}
	models, err := s.QueryCostReportForPrincipal("chat", now.Add(-time.Hour), "model", first)
	if err != nil || len(models) != 2 {
		t.Fatalf("model drilldown=%+v %v", models, err)
	}
	var cost float64
	for _, row := range models {
		cost += row.ListCostUSD
	}
	if math.Abs(cost-0.3) > 1e-9 {
		t.Fatalf("drilldown leaked another identity cost=%f", cost)
	}
	query := RequestHistoryQuery{APIType: "chat", PrincipalID: first, Since: now.Add(-time.Hour), Until: now.Add(time.Hour), Limit: 1}
	page, err := s.QueryRequestHistory(context.Background(), query)
	if err != nil || len(page.Records) != 1 || page.Records[0].PrincipalID != first || page.NextCursor == "" {
		t.Fatalf("history=%+v %v", page, err)
	}
	query.Cursor = page.NextCursor
	query.PrincipalID = other
	if _, err := s.QueryRequestHistory(context.Background(), query); err != ErrHistoryCursor {
		t.Fatalf("cursor switched principal: %v", err)
	}
	query.Cursor = ""
	query.PrincipalID = UnknownPrincipal
	legacy, err := s.QueryRequestHistory(context.Background(), query)
	if err != nil || len(legacy.Records) != 1 || legacy.Records[0].PrincipalID != UnknownPrincipal || legacy.Records[0].PrincipalLabel != UnknownPrincipal {
		t.Fatalf("legacy reassigned=%+v %v", legacy, err)
	}
}
func TestPrincipalCostContextSurvivesFailuresAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.db")
	store, err := NewSQLiteStore(&SQLiteStoreConfig{DBPath: path, RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewMetricsManagerWithPersistence(100, 0.5, store, "chat")
	identity := PrincipalAttributionID("principal:stable-team")
	for i, key := range []string{"old-provider", "rotated-provider"} {
		id := manager.RecordRequestConnectedWithCostContext("https://fake.invalid", key, "openai", "channel", "m", "m", "different-mask", RequestCostContext{PrincipalID: identity})
		if i == 0 {
			manager.RecordRequestFinalizeSuccess("https://fake.invalid", key, "openai", id, &types.Usage{InputTokens: 2, OutputTokens: 1})
		} else {
			manager.RecordRequestFinalizeFailureWithClass("https://fake.invalid", key, "openai", id, FailureClassRetryable)
		}
	}
	manager.Stop()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteStore(&SQLiteStoreConfig{DBPath: path, RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records, err := reopened.LoadRecords(time.Now().Add(-time.Hour), "chat")
	if err != nil || len(records) != 2 {
		t.Fatalf("records=%+v %v", records, err)
	}
	for _, record := range records {
		if record.PrincipalID != identity {
			t.Fatalf("identity lost on finalization/restart: %+v", record)
		}
	}
}
func TestPrincipalMigrationKeepsOldMaskUnattributedAndRejectsRawIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.db")
	s, err := NewSQLiteStore(&SQLiteStoreConfig{DBPath: path, RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DROP INDEX idx_history_principal_id"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("ALTER TABLE request_records DROP COLUMN principal_id"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("PRAGMA user_version = 11"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("INSERT INTO request_records(metrics_key,base_url,key_mask,timestamp,success,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,api_type,proxy_key_mask) VALUES('legacy','https://fake.invalid','mask',?,1,1,1,0,0,'chat','same-mask')", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = NewSQLiteStore(&SQLiteStoreConfig{DBPath: path, RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.batchInsertRecords([]PersistentRecord{{Timestamp: time.Now(), APIType: "chat", PrincipalID: "raw-user@example.invalid", ProxyKeyMask: "same-mask"}}); err != nil {
		t.Fatal(err)
	}
	records, err := s.LoadRecords(time.Now().Add(-time.Hour), "chat")
	if err != nil || len(records) != 2 {
		t.Fatalf("migration records=%+v %v", records, err)
	}
	for _, record := range records {
		if record.PrincipalID != "" {
			t.Fatalf("fabricated legacy/raw attribution=%q", record.PrincipalID)
		}
	}
	rows, err := s.QueryCostReport("chat", time.Now().Add(-time.Hour), "principal")
	if err != nil || len(rows) != 1 || rows[0].GroupKey != UnknownPrincipal || rows[0].TotalRequests != 2 {
		t.Fatalf("unknown rows=%+v %v", rows, err)
	}
}

func TestPrincipalHistoryPreservesLegacyUnscopedCursorFingerprint(t *testing.T) {
	q := RequestHistoryQuery{APIType: "chat", Model: "m", ChannelUID: "c", CorrelationID: "r", Since: time.Unix(1000, 0), Until: time.Unix(2000, 0)}
	const legacy = "3958b2cdb327622d40c3799eb122c9d6dd2dbc4aaa420fbda3887010f25c8ed6"
	if q.fingerprint() != legacy {
		t.Fatal("optional principal filter invalidated an existing unscoped cursor")
	}
	q.PrincipalID = PrincipalAttributionID("principal:team")
	if q.fingerprint() == legacy {
		t.Fatal("principal scope was not bound to the cursor")
	}
}

func TestPrincipalScopeKeepsMissingModelAndKeyGroups(t *testing.T) {
	store := historyTestStore(t)
	now := time.Now()
	identity := PrincipalAttributionID("principal:team")
	if err := store.batchInsertRecords([]PersistentRecord{{Timestamp: now, APIType: "chat", PrincipalID: identity, Model: "known", InputTokens: 2, ListCostUSD: 0.2}, {Timestamp: now, APIType: "chat", PrincipalID: identity, InputTokens: 8, ListCostUSD: 0.8}}); err != nil {
		t.Fatal(err)
	}
	models, err := store.QueryCostReportForPrincipal("chat", now.Add(-time.Hour), "model", identity)
	if err != nil || len(models) != 2 {
		t.Fatalf("missing-model costs dropped: %+v %v", models, err)
	}
	keys, err := store.QueryCostReportForPrincipal("chat", now.Add(-time.Hour), "user", identity)
	if err != nil || len(keys) != 1 || keys[0].GroupKey != "" || keys[0].TotalRequests != 2 || keys[0].ListCostUSD != 1 {
		t.Fatalf("missing-key costs dropped: %+v %v", keys, err)
	}
	missing, err := store.QueryModelCostBreakdownForPrincipal("chat", now.Add(-time.Hour), "model", "", identity)
	if err != nil || len(missing) != 1 || missing[0].Model != "" || missing[0].InputTokens != 8 {
		t.Fatalf("empty group accidentally became unfiltered: %+v %v", missing, err)
	}
}
