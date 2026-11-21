package autopilot

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOTLPLinkSurvivesReloadAndTraceUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.db")
	owner, err := NewProfileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewTraceStoreWithDB(owner.DB())
	if err != nil {
		t.Fatal(err)
	}
	id := "0123456789abcdef0123456789abcdef"
	store.Record(&RoutingDecisionTrace{TraceUID: "rt_otlp", OTLPTraceID: id, RequestKind: "chat", TaskClass: "test", Source: "dry_run", SelectedChannelUID: "channel-old", ShadowChannelUID: "channel-old", Mode: RoutingModeShadow, CreatedAt: time.Now()})
	detail, err := store.GetTraceDetail("rt_otlp")
	if err != nil || detail.OTLPTraceID != id {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	store.Close()
	owner.Close()
	owner, err = NewProfileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, err = NewTraceStoreWithDB(owner.DB())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpdateActualChannel("rt_otlp", "channel-new"); err != nil {
		t.Fatal(err)
	}
	detail, err = store.GetTraceDetail("rt_otlp")
	if err != nil || detail.OTLPTraceID != id {
		t.Fatalf("reload lost link: %+v err=%v", detail, err)
	}
}
