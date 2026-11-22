package entrylease

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/requestscope"
)

func routingManagers(t *testing.T, ttl time.Duration) []*Manager {
	t.Helper()
	clients := isolatedRedis(t)
	epoch := strings.Repeat("a", 64)
	managers := []*Manager{newManager(clients(), "routing-tests", ttl), newManager(clients(), "routing-tests", ttl)}
	for _, m := range managers {
		m.epoch = epoch
		t.Cleanup(func() { _ = m.Close() })
	}
	if err := managers[0].client.Set(context.Background(), managers[0].epochKey(), epoch, 0).Err(); err != nil {
		t.Fatal(err)
	}
	return managers
}
func TestRoutingTwoNodesShareChannelCapacityAndWindow(t *testing.T) {
	ms := routingManagers(t, 2*time.Second)
	ctx := context.Background()
	first, _, err := ms[0].AcquireWindow(ctx, "channel:stable-uid", 1, 2, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, retry, err := ms[1].AcquireWindow(ctx, "channel:stable-uid", 1, 2, 2*time.Second); !errors.Is(err, ErrConcurrency) || retry <= 0 {
		t.Fatalf("cross-node concurrency: %v %v", retry, err)
	}
	first.ParentDone()
	second, _, err := ms[1].AcquireWindow(ctx, "channel:stable-uid", 1, 2, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	second.ParentDone()
	if _, retry, err := ms[0].AcquireWindow(ctx, "channel:stable-uid", 1, 2, 2*time.Second); !errors.Is(err, ErrRPM) || retry <= time.Second {
		t.Fatalf("release erased custom RPM window: %v %v", retry, err)
	}
	if _, _, err := ms[0].AcquireWindow(ctx, "channel:other-uid", 1, 2, 2*time.Second); err != nil {
		t.Fatalf("unrelated channel blocked: %v", err)
	}
}
func TestRoutingStrictAtomicWinnerRetainedAcrossFailureAndLongStream(t *testing.T) {
	ms := routingManagers(t, time.Second)
	ctx := context.Background()
	var wg sync.WaitGroup
	winners := make(chan string, 20)
	leases := make(chan *Lease, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l, _, err := ms[i%2].Acquire(ctx, "strict:shared-session", 0, 0)
			if err != nil {
				t.Error(err)
				return
			}
			winner, err := l.StrictBinding(ctx, []string{"route-a", "route-b"}[i%2], 2*time.Second)
			if err != nil {
				t.Error(err)
				l.ParentDone()
				return
			}
			winners <- winner
			leases <- l
		}(i)
	}
	wg.Wait()
	close(winners)
	close(leases)
	winner := ""
	for actual := range winners {
		if winner == "" {
			winner = actual
		}
		if actual != winner {
			t.Fatalf("split binding %q != %q", actual, winner)
		}
	}
	var active *Lease
	for l := range leases {
		if active == nil {
			active = l
		} else {
			l.ParentDone()
		}
	}
	if active == nil {
		t.Fatal("no accepted lease")
	}
	time.Sleep(3 * time.Second) // exceeds idle TTL; an active stream renews it
	continuation, _, err := ms[1].Acquire(ctx, "strict:shared-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer continuation.ParentDone()
	actual, err := continuation.StrictBinding(ctx, "route-c", 2*time.Second)
	if err != nil || actual != winner {
		t.Fatalf("active stream lost binding: %q %v", actual, err)
	}
	active.ParentDone()
	actual, err = continuation.StrictBinding(ctx, "route-d", 2*time.Second)
	if err != nil || actual != winner {
		t.Fatalf("failed request migrated session: %q %v", actual, err)
	}
}
func TestRoutingEpochLossCancelsLiveWorkAndRejectsNewAdmission(t *testing.T) {
	ms := routingManagers(t, time.Second)
	l, _, err := ms[0].Acquire(context.Background(), "channel:active", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, binding, err := requestscope.Bind(WithLease(context.Background(), l))
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	l.ParentDone() // upstream body still owns the lease
	if err := ms[1].client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms[1].Acquire(context.Background(), "channel:active", 1, 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("recreated empty realm: %v", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("state loss did not cancel live upstream")
	}
	if !errors.Is(requestscope.Cause(ctx), ErrLeaseLost) {
		t.Fatalf("cause %v", requestscope.Cause(ctx))
	}
	if exists := ms[0].client.Exists(context.Background(), ms[0].epochKey()).Val(); exists != 0 {
		t.Fatal("epoch was silently reinitialized")
	}
}

func TestRoutingStrictCapacityIsSharedAndBounded(t *testing.T) {
	ms := routingManagers(t, time.Second)
	ctx := context.Background()
	// Seed the shared reservation index without creating thousands of goroutines.
	_, err := ms[0].client.Eval(ctx, `local now=redis.call('TIME');for i=1,4096 do redis.call('ZADD',KEYS[1],tonumber(now[1])*1000+60000,'existing-'..i) end;return 1`, []string{ms[0].epochKey() + ":sessions"}).Result()
	if err != nil {
		t.Fatal(err)
	}
	lease, _, err := ms[1].Acquire(ctx, "strict:new-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.ParentDone()
	if _, err := lease.StrictBinding(ctx, "new-route", time.Minute); err == nil {
		t.Fatal("cross-node strict session capacity was bypassed")
	}
}

func TestRoutingRequiresExistingEpochAndRejectsChangedEpoch(t *testing.T) {
	ms := routingManagers(t, time.Second)
	ctx := context.Background()
	// A different node epoch cannot join the realm even if it shares credentials.
	ms[1].epoch = strings.Repeat("b", 64)
	if _, _, err := ms[1].Acquire(ctx, "channel:stable", 1, 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("mismatched epoch admitted: %v", err)
	}
	if err := ms[0].client.Del(ctx, ms[0].epochKey()).Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms[0].Acquire(ctx, "strict:session", 0, 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing epoch admitted: %v", err)
	}
}

func TestRoutingStrictBindingLossCannotRecreateAnotherRoute(t *testing.T) {
	ms := routingManagers(t, time.Second)
	ctx := context.Background()
	first, _, err := ms[0].Acquire(ctx, "strict:lost-binding", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = first.StrictBinding(ctx, "original-resource", time.Minute); err != nil {
		t.Fatal(err)
	}
	first.ParentDone()
	if err = ms[0].client.Del(ctx, first.strictKey).Err(); err != nil {
		t.Fatal(err)
	}
	next, _, err := ms[1].Acquire(ctx, "strict:lost-binding", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer next.ParentDone()
	if _, err = next.StrictBinding(ctx, "", time.Minute); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing binding treated as unused session: %v", err)
	}
	// Even a fresh lease proposing another resource cannot overwrite the live tombstone.
	replacement, _, err := ms[1].Acquire(ctx, "strict:lost-binding", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.ParentDone()
	if _, err = replacement.StrictBinding(ctx, "different-resource", time.Minute); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("lost binding migrated: %v", err)
	}
}
