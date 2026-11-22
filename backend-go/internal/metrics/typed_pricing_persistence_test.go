package metrics

import (
	"context"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/types"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestTypedPricingFrozenVersionPersistsInHistoryWithoutRepricing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pricing.db")
	store, err := NewSQLiteStore(&SQLiteStoreConfig{DBPath: path, RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewMetricsManagerWithPersistence(100, .5, store, "images")
	input, ttl, image := 1.0, 2.0, .1
	pricing := &config.ModelPricing{InputCacheMissPrice: &input, CacheCreation5mPrice: &ttl, ImagePrices: []config.ImagePriceRule{{Price: &image}}}
	version := config.PricingVersionID(pricing)
	id := manager.RecordRequestConnectedWithCostContext("https://pricing.invalid", "fake", "openai", "ch_pricing", "test-pricing-model", "test-pricing-model", "", RequestCostContext{ListPricing: pricing, EffectiveCostAvailable: true, EffectiveCostMultiplier: 1})
	input = 100
	ttl = 200
	image = 10
	manager.RecordRequestFinalizeSuccess("https://pricing.invalid", "fake", "openai", id, &types.Usage{InputTokens: 1000000, CacheCreationInputTokens: 1000000, CacheCreation5mInputTokens: 1000000, ImageUnits: []types.ImageUsage{{Count: 3}}})
	store.flush()
	rows, err := store.LoadRecords(time.Now().Add(-time.Hour), "images")
	if err != nil || len(rows) != 1 {
		t.Fatalf("records=%+v err=%v", rows, err)
	}
	if math.Abs(rows[0].ListCostUSD-3.3) > 1e-9 || rows[0].PricingVersionID != version {
		t.Fatalf("price mutation changed frozen bill: %+v", rows[0])
	}
	page, err := store.QueryRequestHistory(context.Background(), RequestHistoryQuery{APIType: "images", Since: time.Now().Add(-time.Hour), Until: time.Now().Add(time.Hour), Limit: 10})
	if err != nil || len(page.Records) != 1 || page.Records[0].PricingVersionID != version {
		t.Fatalf("history omitted price version: %+v %v", page, err)
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
	persisted, err := reopened.LoadRecords(time.Now().Add(-time.Hour), "images")
	if err != nil || len(persisted) != 1 || persisted[0].PricingVersionID != version || math.Abs(persisted[0].ListCostUSD-3.3) > 1e-9 {
		t.Fatalf("reload repriced history: %+v %v", persisted, err)
	}
}
