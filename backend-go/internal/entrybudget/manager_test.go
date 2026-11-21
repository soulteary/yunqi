package entrybudget

import (
	"context"
	"github.com/soulteary/yunqi/internal/requestscope"
	"path/filepath"
	"testing"
	"time"
)

func TestLogicalBudgetWaitsForBackgroundBodyAndChargesOnce(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "ledger.db"))
	defer m.Close()
	p := testPolicy(t, 100, 100)
	s, err := m.Start(context.Background(), p, "logical")
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithSession(context.Background(), s)
	_, binding, err := requestscope.Bind(ctx)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := s.Begin(ctx, quote("ignored", 40))
	if err != nil {
		t.Fatal(err)
	}
	s.ParentDone()
	store, _ := m.Store()
	snap, err := store.Snapshot(ctx, p, time.Now())
	if err != nil || snap.Reserved != 40 || snap.Spent != 0 {
		t.Fatal("HTTP completion settled unfinished background work")
	}
	actual := int64(7)
	if err := attempt.Complete(&actual, "provider_usage"); err != nil {
		t.Fatal(err)
	}
	binding.Close()
	binding.Close()
	snap, err = store.Snapshot(ctx, p, time.Now())
	if err != nil || snap.Reserved != 0 || snap.Spent != 7 {
		t.Fatalf("logical accounting %+v %v", snap, err)
	}
}

func TestBudgetRetryAndRacingAllProviderAttemptsSettleOneLogicalRequest(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "ledger.db"))
	defer m.Close()
	p := testPolicy(t, 100, 100)
	s, err := m.Start(context.Background(), p, "racing")
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithSession(context.Background(), s)
	_, first, err := requestscope.Bind(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := s.Begin(ctx, quote("", 30))
	if err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if err := rejected.Complete(&zero, "upstream_rejected"); err != nil {
		t.Fatal(err)
	}
	winner, err := s.Begin(ctx, quote("", 40))
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := requestscope.Bind(ctx)
	if err != nil {
		t.Fatal(err)
	}
	loser, err := s.Begin(ctx, quote("", 40))
	if err != nil {
		t.Fatal(err)
	}
	actual := int64(7)
	if err := winner.Complete(&actual, "provider_usage"); err != nil {
		t.Fatal(err)
	}
	s.ParentDone()
	first.Close()
	store, _ := m.Store()
	state, err := store.Snapshot(context.Background(), p, time.Now())
	if err != nil || state.Spent != 0 || state.Reserved != 47 {
		t.Fatalf("winner ended loser still active %+v %v", state, err)
	}
	if err := loser.Complete(nil, "racing_loser_usage_unavailable"); err != nil {
		t.Fatal(err)
	}
	second.Close()
	second.Close()
	state, err = store.Snapshot(context.Background(), p, time.Now())
	if err != nil || state.Spent != 47 || state.Reserved != 0 {
		t.Fatalf("all actual attempts charged once %+v %v", state, err)
	}
	records, err := store.Requests(context.Background(), p.PrincipalID, 10)
	if err != nil || len(records) != 1 || records[0].Reason != "conservative_usage_unavailable" {
		t.Fatalf("logical audit %+v %v", records, err)
	}
}
