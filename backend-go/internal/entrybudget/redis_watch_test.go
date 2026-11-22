package entrybudget

import (
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"sync"
	"testing"
	"time"
)

// Pause after a real GET has captured a response, then commit through the other
// independent Redis connection before subsequent validation reads. WATCH does
// not give those reads a snapshot; only EXEC can validate their consistency.
type budgetReadInterleave struct {
	key  string
	once sync.Once
	run  func() error
	err  error
}

func (h *budgetReadInterleave) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *budgetReadInterleave) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *budgetReadInterleave) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if cmd.Name() == "get" && len(cmd.Args()) > 1 && cmd.Args()[1] == h.key {
			h.once.Do(func() { h.err = h.run() })
		}
		return err
	}
}
func TestRedisBudgetFirstPrincipalCreationRetriesInconsistentValidation(t *testing.T) {
	first, second, _ := sharedTestStores(t)
	ctx := context.Background()
	p := sharedPolicy()
	hook := &budgetReadInterleave{key: first.shared.prefix + "p:" + principalDigest(p.PrincipalID), run: func() error { return second.Start(ctx, p, "concurrent-first-request", time.Now()) }}
	first.shared.client.AddHook(hook)
	if err := first.Start(ctx, p, "original-request", time.Now()); err != nil {
		t.Fatalf("concurrent creation treated as damaged storage: %v (interleave=%v)", err, hook.err)
	}
	if hook.err != nil {
		t.Fatal(hook.err)
	}
	records, err := second.Requests(ctx, p.PrincipalID, 10)
	if err != nil || len(records) != 2 {
		t.Fatalf("concurrent starts lost: %+v %v", records, err)
	}
}
func TestRedisBudgetQuotaErrorRechecksWatchBeforeRejecting(t *testing.T) {
	first, second, _ := sharedTestStores(t)
	ctx := context.Background()
	p := sharedPolicy()
	now := time.Now()
	if err := first.Start(ctx, p, "held", now); err != nil {
		t.Fatal(err)
	}
	if err := first.Reserve(ctx, p, "held", sharedQuote("held.a1", 1000), now); err != nil {
		t.Fatal(err)
	}
	if err := first.Start(ctx, p, "next", now); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	hook := &budgetReadInterleave{key: first.shared.prefix + "p:" + principalDigest(p.PrincipalID), run: func() error {
		if err := second.CompleteAccounting(ctx, "held", "held.a1", &zero, "provider_usage", "provider", json.RawMessage(`{"input_tokens":0,"output_tokens":0}`)); err != nil {
			return err
		}
		return second.Finalize(ctx, "held", "", time.Now())
	}}
	first.shared.client.AddHook(hook)
	if err := first.Reserve(ctx, p, "next", sharedQuote("next.a1", 400), now); err != nil {
		t.Fatalf("stale quota rejection after concurrent release: %v (interleave=%v)", err, hook.err)
	}
	if hook.err != nil {
		t.Fatal(hook.err)
	}
	snapshot, err := second.Snapshot(ctx, p, time.Now())
	if err != nil || snapshot.Reserved != 400 || snapshot.Spent != 0 {
		t.Fatalf("unexpected accounting: %+v %v", snapshot, err)
	}
}

func TestRedisBudgetParentCancellationBoundsTransaction(t *testing.T) {
	for _, mode := range []string{"cancel", "short_deadline"} {
		t.Run(mode, func(t *testing.T) {
			first, second, _ := sharedTestStores(t)
			p := sharedPolicy()
			var ctx context.Context
			var cancel context.CancelFunc
			if mode == "short_deadline" {
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			reached := make(chan struct{})
			hook := &budgetReadInterleave{key: first.shared.prefix + "p:" + principalDigest(p.PrincipalID), run: func() error { close(reached); <-ctx.Done(); return ctx.Err() }}
			first.shared.client.AddHook(hook)
			result := make(chan error, 1)
			go func() { result <- first.Start(ctx, p, "must-not-be-created", time.Now()) }()
			select {
			case <-reached:
			case <-time.After(time.Second):
				cancel()
				t.Fatal("test did not reach the watched read")
			}
			if mode == "cancel" {
				cancel()
			}
			stopped := time.Now()
			select {
			case err := <-result:
				if err != ErrUnavailable {
					t.Fatalf("cancelled transaction error=%v", err)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("inner five-second budget outlived parent cancellation")
			}
			if time.Since(stopped) > 500*time.Millisecond {
				t.Fatal("parent deadline did not bound transaction")
			}
			records, err := second.Requests(context.Background(), p.PrincipalID, 10)
			if err != nil || len(records) != 0 {
				t.Fatalf("cancelled transaction wrote accounting: %+v %v", records, err)
			}
		})
	}
}

func TestRedisBudgetClientCancellationStillSettlesSentAttempt(t *testing.T) {
	observer, _, clients := sharedTestStores(t)
	manager, err := NewRedisManager(clients(), "test", "fixed-original-epoch")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	p := sharedPolicy()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := manager.Start(ctx, p, "already-sent")
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := session.Begin(ctx, sharedQuote("assigned-by-session", 700))
	if err != nil {
		t.Fatal(err)
	}
	// The provider has already been called. Accounting uses its own bounded
	// context, so a disconnected caller cannot erase a measured charge.
	cancel()
	charge := int64(300)
	if err := attempt.CompleteAccounting(&charge, "provider_usage", "provider", json.RawMessage(`{"input_tokens":2,"output_tokens":1}`)); err != nil {
		t.Fatal(err)
	}
	session.ParentDone()
	snapshot, err := observer.Snapshot(context.Background(), p, time.Now())
	if err != nil || snapshot.Spent != 300 || snapshot.Reserved != 0 {
		t.Fatalf("cancelled caller erased sent usage: %+v %v", snapshot, err)
	}
}
