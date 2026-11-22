package telemetry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDiagnosticCanceledUsageIsBoundedPrivateAndDetached(t *testing.T) {
	registry := NewRegistry()
	ctx, timing := WithTiming(context.Background())
	_, diagnostic := registry.startDiagnostics(ctx, "responses", timing)
	for i := 0; i < 10000; i++ {
		diagnostic.SetUsageAfterCancel(DiagnosticUsageSummary{AttemptID: "https://private.invalid/key", Protocol: "secret-protocol", UsageSource: "secret-source", Complete: true, Reason: "secret-error", CacheTTL: "secret-ttl", InputTokens: -1, OutputTokens: 3})
	}
	value := diagnostic.Snapshot()
	if value.UsageAfterCancel == nil || value.UsageAfterCancel.InputTokens != 0 || value.UsageAfterCancel.Complete {
		t.Fatalf("unsafe summary %#v", value.UsageAfterCancel)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 2048 || strings.Contains(string(encoded), "secret-") || strings.Contains(string(encoded), "private.invalid") || strings.Contains(string(encoded), "rawUsage") {
		t.Fatalf("unbounded/private summary %s", encoded)
	}
	value.UsageAfterCancel.OutputTokens = 999
	again := diagnostic.Snapshot()
	if again.UsageAfterCancel.OutputTokens != 3 {
		t.Fatal("summary aliases retained state")
	}
}
