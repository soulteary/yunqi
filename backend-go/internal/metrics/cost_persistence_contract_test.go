package metrics

import (
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/types"
	"math"
	"path/filepath"
	"testing"
	"time"
)

// Configured prices must survive final usage accounting and disk persistence,
// even when the client-facing model has no built-in price.
func TestConfiguredCostPersistsMappedModelAndCacheUsage(t *testing.T) {
	for _, free := range []bool{false, true} {
		t.Run(map[bool]string{false: "priced", true: "explicit_free"}[free], func(t *testing.T) {
			store, err := NewSQLiteStore(&SQLiteStoreConfig{DBPath: filepath.Join(t.TempDir(), "metrics.db"), RetentionDays: 7})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			manager := NewMetricsManagerWithPersistence(100, .5, store, "responses")
			defer manager.Stop()
			input, output, cache := 2.0, 4.0, .5
			if free {
				input, output, cache = 0, 0, 0
			}
			pricing := &config.ModelPricing{Unit: "per_million_tokens", Currency: "USD", InputCacheMissPrice: &input, OutputPrice: &output, InputCacheHitPrice: &cache}
			id := manager.RecordRequestConnectedWithCostContext("https://mock.invalid", "fake-key", "responses", "ch_cost", "private-actual-model", "client-alias", "fake-***", RequestCostContext{ListPricing: pricing, EffectiveCostAvailable: true, EffectiveCostMultiplier: .25})
			manager.RecordRequestFinalizeSuccess("https://mock.invalid", "fake-key", "responses", id, &types.Usage{InputTokens: 1000000, OutputTokens: 500000, CacheCreationInputTokens: 250000, CacheReadInputTokens: 2000000})
			store.flush()
			records, err := store.LoadRecords(time.Now().Add(-time.Minute), "responses")
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != 1 {
				t.Fatalf("records=%d", len(records))
			}
			got := records[0]
			want := 5.5
			if free {
				want = 0
			}
			if math.Abs(got.ListCostUSD-want) > 1e-9 || math.Abs(got.EffectiveCostUSD-want*.25) > 1e-9 || !got.EffectiveCostAvailable {
				t.Fatalf("persisted costs list=%v effective=%v available=%v", got.ListCostUSD, got.EffectiveCostUSD, got.EffectiveCostAvailable)
			}
			if got.Model != "private-actual-model" || got.RouteModel != "client-alias" || got.CacheReadTokens != 2000000 {
				t.Fatalf("mapped identity/cache lost: %#v", got)
			}
		})
	}
}

func TestLegacyCostBreakdownDistinguishesSnapshotsAndFilters(t *testing.T) {
	store, err := NewSQLiteStore(&SQLiteStoreConfig{DBPath: filepath.Join(t.TempDir(), "legacy.db"), RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now()
	for _, key := range []string{"wanted", "other"} {
		for i := 0; i < 2; i++ {
			store.AddRecord(PersistentRecord{Timestamp: now, APIType: "responses", Model: "same-model", ProxyKeyMask: key, InputTokens: int64(100 + i), ListCostUSD: 0})
		}
	}
	store.flush()
	if _, err := store.db.Exec("UPDATE request_records SET list_cost_usd=NULL WHERE input_tokens=101"); err != nil {
		t.Fatal(err)
	}
	rows, err := store.QueryModelCostBreakdown("responses", now.Add(-time.Minute), "user", "wanted")
	if err != nil {
		t.Fatal(err)
	}
	legacyTokens := int64(0)
	for _, row := range rows {
		if !row.HasSnapshot {
			legacyTokens += row.InputTokens
		}
	}
	if len(rows) != 2 || legacyTokens != 101 {
		t.Fatalf("legacy subset=%#v", rows)
	}
	rows, err = store.QueryModelCostBreakdown("messages", now.Add(-time.Minute), "user", "wanted")
	if err != nil || len(rows) != 0 {
		t.Fatalf("type filter rows=%#v err=%v", rows, err)
	}
	rows, err = store.QueryModelCostBreakdown("responses", now.Add(time.Hour), "user", "wanted")
	if err != nil || len(rows) != 0 {
		t.Fatalf("time filter rows=%#v err=%v", rows, err)
	}
}
