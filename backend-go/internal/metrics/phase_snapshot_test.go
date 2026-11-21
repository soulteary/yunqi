package metrics

import (
	"context"
	"github.com/soulteary/yunqi/internal/telemetry"
	"testing"
)

func TestChannelLogPhaseSnapshotIsDetached(t *testing.T) {
	ctx, tl := telemetry.WithTiming(context.Background())
	end := telemetry.Phase(ctx, "queue")
	end()
	tl.Finish()
	store := NewChannelLogStore()
	store.Record("k", &ChannelLog{RequestID: "r", TimingSource: tl})
	first := store.Get("k")
	if len(first) != 1 || first[0].PhaseTiming == nil {
		t.Fatal("missing phase timing")
	}
	first[0].PhaseTiming.StagesMs["queue"] = 999999
	second := store.GetMerged([]string{"k"})
	if second[0].PhaseTiming.StagesMs["queue"] == 999999 {
		t.Fatal("snapshot alias mutated store")
	}
}
