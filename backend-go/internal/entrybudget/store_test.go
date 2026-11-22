package entrybudget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testPolicy(t *testing.T, limit, cap int64) Policy {
	t.Helper()
	p, err := (Policy{PrincipalID: "team", Period: "lifetime", LimitNanoUSD: limit, RequestCapNanoUSD: cap}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "budget.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func quote(id string, n int64) AttemptQuote {
	return AttemptQuote{ID: id, Model: "model", ChannelUID: "channel", PricingVersion: "v1", Pricing: json.RawMessage(`{"outputPrice":2}`), Amount: n}
}
func TestBudgetReleasesUnusedReservationAndSettlesOnce(t *testing.T) {
	s := testStore(t)
	p := testPolicy(t, 100, 100)
	ctx := context.Background()
	now := time.Unix(1000, 0)
	if err := s.Start(ctx, p, "request", now); err != nil {
		t.Fatal(err)
	}
	q := quote("attempt", 50)
	if err := s.Reserve(ctx, p, "request", q, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, p, "request", q, now); err != nil {
		t.Fatal(err)
	}
	actual := int64(7)
	if err := s.Complete(ctx, "request", "attempt", &actual, "provider_usage"); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(ctx, p, now)
	if err != nil || snap.Reserved != 7 || snap.Spent != 0 {
		t.Fatalf("hold %+v %v", snap, err)
	}
	for i := 0; i < 2; i++ {
		if err := s.Finalize(ctx, "request", "", now); err != nil {
			t.Fatal(err)
		}
	}
	actual = 99
	if err := s.Complete(ctx, "request", "attempt", &actual, "duplicate"); err != nil {
		t.Fatal(err)
	}
	snap, err = s.Snapshot(ctx, p, now)
	if err != nil || snap.Reserved != 0 || snap.Spent != 7 {
		t.Fatalf("settlement %+v %v", snap, err)
	}
	records, err := s.Attempts(ctx, "request")
	if err != nil || len(records) != 1 || records[0].PricingVersion != "v1" || records[0].ChargedNanoUSD != 7 || string(records[0].Pricing) != string(q.Pricing) {
		t.Fatalf("lost immutable accounting snapshot: %+v %v", records, err)
	}
	if err := s.Start(ctx, p, "rejected", now); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, p, "rejected", quote("failed", 20), now); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if err := s.Complete(ctx, "rejected", "failed", &zero, "upstream_rejected"); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, "rejected", "", now); err != nil {
		t.Fatal(err)
	}
	snap, err = s.Snapshot(ctx, p, now)
	if err != nil || snap.Reserved != 0 || snap.Spent != 7 {
		t.Fatal("failed request consumed budget")
	}
}
func TestBudgetConcurrentReservationsAcrossTwoStoresNeverExceedLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "budget.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	stores := []*Store{first, second}
	p := testPolicy(t, 10, 1)
	now := time.Unix(1000, 0)
	ctx := context.Background()
	var successes atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			s := stores[index%2]
			id := fmt.Sprintf("r%d", index)
			if err := s.Start(ctx, p, id, now); err != nil {
				t.Error(err)
				return
			}
			err := s.Reserve(ctx, p, id, quote(id+"-a", 1), now)
			if err == nil {
				successes.Add(1)
				one := int64(1)
				if err := s.Complete(ctx, id, id+"-a", &one, "provider_usage"); err != nil {
					t.Error(err)
				}
			} else if !errors.Is(err, ErrQuota) {
				t.Error(err)
			}
			if err := s.Finalize(ctx, id, "", now); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	snap, err := first.Snapshot(ctx, p, now)
	if err != nil || successes.Load() != 10 || snap.Spent != 10 || snap.Reserved != 0 {
		t.Fatalf("cross-instance overspend: successes=%d snapshot=%+v err=%v", successes.Load(), snap, err)
	}
	p.LimitNanoUSD = 11
	if err := first.Start(ctx, p, "admin-raised", now); err != nil {
		t.Fatal(err)
	}
	if err := first.Reserve(ctx, p, "admin-raised", quote("raised", 1), now); err != nil {
		t.Fatal("administrator limit increase not effective", err)
	}
}
func TestBudgetRecoveryOnlyChargesExpiredLeases(t *testing.T) {
	s := testStore(t)
	p := testPolicy(t, 100, 100)
	ctx := context.Background()
	now := time.Unix(1000, 0)
	for _, id := range []string{"orphan", "live"} {
		if err := s.Start(ctx, p, id, now); err != nil {
			t.Fatal(err)
		}
		if err := s.Reserve(ctx, p, id, quote(id, 10), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Heartbeat(ctx, "live", now.Add(50*time.Second)); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(ctx, p, now.Add(61*time.Second))
	if err != nil || snap.Spent != 10 || snap.Reserved != 10 {
		t.Fatalf("incorrect lease recovery: %+v %v", snap, err)
	}
	if err := s.Finalize(ctx, "orphan", "duplicate", now.Add(62*time.Second)); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if err := s.Complete(ctx, "live", "live", &zero, "authoritative_zero"); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, "live", "", now.Add(62*time.Second)); err != nil {
		t.Fatal(err)
	}
	snap, err = s.Snapshot(ctx, p, now.Add(62*time.Second))
	if err != nil || snap.Spent != 10 || snap.Reserved != 0 {
		t.Fatal("recovery or zero usage charged twice")
	}
}

func TestRollingBudgetUsesActualSlidingWindowAndKeepsActiveHolds(t *testing.T) {
	s := testStore(t)
	p := testPolicy(t, 20, 20)
	p.Period = "rolling"
	p.RollingSeconds = 10
	ctx := context.Background()
	at := func(n int64) time.Time { return time.Unix(n, 0) }
	for _, r := range []struct {
		id           string
		when, amount int64
	}{{"active", 100, 5}, {"settled", 100, 10}} {
		if err := s.Start(ctx, p, r.id, at(r.when)); err != nil {
			t.Fatal(err)
		}
		if err := s.Reserve(ctx, p, r.id, quote(r.id, r.amount), at(r.when)); err != nil {
			t.Fatal(err)
		}
	}
	ten := int64(10)
	if err := s.Complete(ctx, "settled", "settled", &ten, "provider_usage"); err != nil {
		t.Fatal(err)
	}
	if err := s.Finalize(ctx, "settled", "", at(100)); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx, p, "too-soon", at(109)); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, p, "too-soon", quote("too-soon", 6), at(109)); !errors.Is(err, ErrQuota) {
		t.Fatalf("sliding budget bypass %v", err)
	}
	if err := s.Start(ctx, p, "next-window", at(111)); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, p, "next-window", quote("next-window", 16), at(111)); !errors.Is(err, ErrQuota) {
		t.Fatalf("active hold fell out of rolling window: %v", err)
	}
	if err := s.Reserve(ctx, p, "next-window", quote("fifteen", 15), at(111)); err != nil {
		t.Fatal(err)
	}
}

func TestBudgetAuditOwnerLookupDoesNotDependOnRecentPage(t *testing.T) {
	s := testStore(t)
	p := testPolicy(t, 100, 100)
	ctx := context.Background()
	now := time.Unix(1000, 0)
	if err := s.Start(ctx, p, "a-old", now); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 201; i++ {
		if err := s.Start(ctx, p, fmt.Sprintf("z-new-%03d", i), now); err != nil {
			t.Fatal(err)
		}
	}
	recent, err := s.Requests(ctx, p.PrincipalID, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recent {
		if r.ID == "a-old" {
			t.Fatal("fixture did not leave recent page")
		}
	}
	if owned, err := s.RequestOwned(ctx, "a-old", p.PrincipalID); err != nil || !owned {
		t.Fatalf("old own record denied %v %v", owned, err)
	}
	if owned, err := s.RequestOwned(ctx, "a-old", "different-owner"); err != nil || owned {
		t.Fatalf("cross-owner record exposed %v %v", owned, err)
	}
}
