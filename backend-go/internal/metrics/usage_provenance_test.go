package metrics

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/types"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageProvenanceFinalizationRoundTrip(t *testing.T) {
	store, err := NewSQLiteStore(&SQLiteStoreConfig{DBPath: filepath.Join(t.TempDir(), "usage.db"), RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager := NewMetricsManagerWithPersistence(100, .5, store, "responses")
	defer manager.Stop()
	u := &types.Usage{InputTokens: 7, OutputTokens: 9, Provenance: &types.UsageProvenance{UsageSource: "mixed", Protocol: "responses", EstimatedFields: []string{"output_tokens"}, Reason: "missing_output", RawUsage: json.RawMessage(`{"input_tokens":7}`)}}
	id := manager.RecordRequestConnectedWithCostContext("https://mock.invalid", "fake", "responses", "channel", "model", "alias", "mask", RequestCostContext{})
	manager.RecordRequestFinalizeSuccess("https://mock.invalid", "fake", "responses", id, u)
	// The ledger snapshot must not alias mutable caller-owned metadata.
	u.Provenance.EstimatedFields[0] = "changed"
	store.flush()
	records, err := store.LoadRecords(time.Now().Add(-time.Minute), "responses")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records=%d", len(records))
	}
	got := records[0].UsageProvenance
	if got == nil || got.UsageSource != "mixed" || got.Protocol != "responses" || got.EstimatedFields[0] != "output_tokens" || string(got.RawUsage) != `{"input_tokens":7}` {
		t.Fatalf("%+v", got)
	}
}

func TestUsageProvenanceLegacyUnknown(t *testing.T) {
	p := unmarshalUsageProvenance("{}")
	if p.UsageSource != "unknown" || p.Reason == "" {
		t.Fatalf("%+v", p)
	}
}
