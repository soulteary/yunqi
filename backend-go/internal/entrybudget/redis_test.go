package entrybudget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func isolatedRedis(t *testing.T) func() *redis.Client {
	t.Helper()
	binary := os.Getenv("YUNQI_TEST_REDIS_SERVER")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("redis-server")
		if err != nil {
			t.Skip("Redis executable unavailable; set YUNQI_TEST_REDIS_SERVER for isolated integration test")
		}
	}
	dir, dirErr := os.MkdirTemp("/tmp", "yqr-")
	if dirErr != nil {
		t.Fatal(dirErr)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "redis.sock")
	output, err := os.Create(filepath.Join(dir, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "--port", "0", "--unixsocket", socket, "--unixsocketperm", "700", "--save", "", "--appendonly", "no", "--daemonize", "no", "--dir", dir)
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		_ = output.Close()
	})
	newClient := func() *redis.Client {
		return redis.NewClient(&redis.Options{Network: "unix", Addr: socket, ContextTimeoutEnabled: true, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, MaxRetries: -1, DisableIdentity: true})
	}
	probe := newClient()
	defer probe.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if probe.Ping(context.Background()).Err() == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("isolated Redis did not become ready")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return newClient
}
func sharedTestStores(t *testing.T) (*Store, *Store, func() *redis.Client) {
	t.Helper()
	clients := isolatedRedis(t)
	bootstrap := clients()
	defer bootstrap.Close()
	// Test servers have no TCP listener; only this explicit client is injected.
	namespace := "test"
	epoch := "fixed-original-epoch"
	prefix := redisBudgetPrefix(namespace)
	if err := bootstrap.Set(context.Background(), prefix+"epoch", epoch, 0).Err(); err != nil {
		t.Fatal(err)
	}
	bootstrap.HSet(context.Background(), prefix+"registry", "schema", "1")
	bootstrap.HSet(context.Background(), prefix+"requests", "schema", "1")
	one, err := OpenRedis(clients(), namespace, epoch)
	if err != nil {
		t.Fatal(err)
	}
	two, err := OpenRedis(clients(), namespace, epoch)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { one.Close(); two.Close() })
	// Production NewRedisManager validates this namespace before accepting
	// concurrent requests. Warm transport identically without creating a principal.
	if err := one.shared.ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := two.shared.ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	return one, two, clients
}
func sharedPolicy() Policy {
	p, _ := (Policy{PrincipalID: "team", Period: "month", LimitNanoUSD: 1000, RequestCapNanoUSD: 1000}).Normalize()
	return p
}
func sharedQuote(id string, amount int64) AttemptQuote {
	return AttemptQuote{ID: id, Model: "model", ChannelUID: "channel", PricingVersion: "captured-v1", Pricing: json.RawMessage(`{"input":0.01}`), Amount: amount}
}
func TestRedisBudgetTwoNodesReserveOneTeamAtomically(t *testing.T) {
	testRedisBudgetReservationRace(t, false)
}
func TestRedisBudgetInitializedNodesReserveOneTeamAtomically(t *testing.T) {
	testRedisBudgetReservationRace(t, true)
}
func testRedisBudgetReservationRace(t *testing.T, initialized bool) {
	t.Helper()
	one, two, _ := sharedTestStores(t)
	stores := []*Store{one, two}
	p := sharedPolicy()
	ctx := context.Background()
	if initialized {
		if _, err := one.Snapshot(ctx, p, time.Now()); err != nil {
			t.Fatal(err)
		}
	} else if count, err := one.shared.client.Exists(ctx, one.shared.prefix+"p:"+principalDigest(p.PrincipalID)).Result(); err != nil || count != 0 {
		t.Fatalf("first-principal fixture already initialized: %d %v", count, err)
	}
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := stores[i%2]
			id := fmt.Sprintf("request-%02d", i)
			if err := s.Start(ctx, p, id, time.Now()); err != nil {
				t.Errorf("start: %v", err)
				return
			}
			err := s.Reserve(ctx, p, id, sharedQuote(id+".a1", 100), time.Now())
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrQuota) {
				t.Errorf("reserve: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 10 {
		t.Fatalf("accepted=%d want 10", accepted.Load())
	}
	for _, s := range stores {
		snap, err := s.Snapshot(ctx, p, time.Now())
		if err != nil || snap.Spent != 0 || snap.Reserved != 1000 {
			t.Fatalf("snapshot=%+v err=%v", snap, err)
		}
	}
}
func TestRedisBudgetAttemptsCapturePricesAndSettleExactlyOnce(t *testing.T) {
	one, two, _ := sharedTestStores(t)
	ctx := context.Background()
	p := sharedPolicy()
	now := time.Now()
	if err := one.Start(ctx, p, "retry", now); err != nil {
		t.Fatal(err)
	}
	for _, q := range []AttemptQuote{sharedQuote("retry.a1", 200), sharedQuote("retry.a2", 300)} {
		if err := one.Reserve(ctx, p, "retry", q, now); err != nil {
			t.Fatal(err)
		}
	}
	actual := int64(150)
	if err := two.CompleteAccounting(ctx, "retry", "retry.a1", &actual, "provider_usage", "provider", json.RawMessage(`{"input_tokens":10}`)); err != nil {
		t.Fatal(err)
	}
	if err := two.CompleteAccounting(ctx, "retry", "retry.a2", nil, "canceled", "unknown", nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := two.Finalize(ctx, "retry", "", now); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := one.Snapshot(ctx, p, now)
	if err != nil || snap.Spent != 450 || snap.Reserved != 0 {
		t.Fatalf("snapshot=%+v err=%v", snap, err)
	}
	records, err := two.Attempts(ctx, "retry")
	if err != nil || len(records) != 2 || records[0].PricingVersion != "captured-v1" || records[0].UsageSource != "provider" || records[1].Status != "conservative" {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	if owned, err := two.RequestOwned(ctx, "retry", "other-team"); err != nil || owned {
		t.Fatalf("cross-team record access owned=%t err=%v", owned, err)
	}
}
func TestRedisBudgetRestartAndExpiredReservationStayCharged(t *testing.T) {
	one, two, clients := sharedTestStores(t)
	p := sharedPolicy()
	ctx := context.Background()
	if err := one.Start(ctx, p, "abandoned", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := one.Reserve(ctx, p, "abandoned", sharedQuote("abandoned.a1", 700), time.Now()); err != nil {
		t.Fatal(err)
	}
	admin := clients()
	defer admin.Close()
	key := one.shared.prefix + "p:" + principalDigest(p.PrincipalID)
	raw, err := admin.Get(ctx, key).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var state sharedState
	json.Unmarshal(raw, &state)
	state.Requests["abandoned"].LeaseUntil = 1
	raw, _ = json.Marshal(state)
	admin.Set(ctx, key, raw, 0)
	// A fresh client sees a persisted abandoned hold and conservatively settles it.
	fresh, err := OpenRedis(clients(), "test", "fixed-original-epoch")
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	snapshot, err := fresh.Snapshot(ctx, p, time.Now())
	if err != nil || snapshot.Spent != 700 || snapshot.Reserved != 0 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if err := two.Start(ctx, p, "next", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := two.Reserve(ctx, p, "next", sharedQuote("next.a1", 301), time.Now()); err != ErrQuota {
		t.Fatalf("overspend=%v", err)
	}
	if err := one.Heartbeat(ctx, "abandoned", time.Now()); err != ErrClosed {
		t.Fatalf("expired lease resurrected: %v", err)
	}
}
func TestRedisBudgetStateLossAndClosedBackendFailClosed(t *testing.T) {
	for _, part := range []string{"epoch", "registry", "requests", "principal"} {
		t.Run(part, func(t *testing.T) {
			one, two, clients := sharedTestStores(t)
			ctx := context.Background()
			p := sharedPolicy()
			if err := one.Start(ctx, p, "initial", time.Now()); err != nil {
				t.Fatal(err)
			}
			admin := clients()
			defer admin.Close()
			key := one.shared.prefix + part
			if part == "principal" {
				key = one.shared.prefix + "p:" + principalDigest(p.PrincipalID)
			}
			admin.Del(ctx, key)
			if err := two.Start(ctx, p, "new", time.Now()); err != ErrUnavailable {
				t.Fatalf("lost %s accepted: %v", part, err)
			}
		})
	}
	one, _, _ := sharedTestStores(t)
	one.shared.client.Close()
	if err := one.Start(context.Background(), sharedPolicy(), "offline", time.Now()); err != ErrUnavailable {
		t.Fatalf("backend failure=%v", err)
	}
}
func TestRedisBudgetUsesExactInt64BeyondFloatPrecision(t *testing.T) {
	one, two, _ := sharedTestStores(t)
	ctx := context.Background()
	p := sharedPolicy()
	p.LimitNanoUSD = 1 << 54
	p.RequestCapNanoUSD = p.LimitNanoUSD
	amount := int64(1<<53) + 1
	if err := one.Start(ctx, p, "precise", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := one.Reserve(ctx, p, "precise", sharedQuote("precise.a1", amount), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := two.Complete(ctx, "precise", "precise.a1", &amount, "provider"); err != nil {
		t.Fatal(err)
	}
	if err := two.Finalize(ctx, "precise", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := one.Snapshot(ctx, p, time.Now())
	if err != nil || snapshot.Spent != amount {
		t.Fatalf("rounded amount: %+v %v", snapshot, err)
	}
}
func TestRedisBudgetInitializationDoesNotRepairExistingNamespace(t *testing.T) {
	one, _, clients := sharedTestStores(t)
	ctx := context.Background()
	admin := clients()
	defer admin.Close()
	if err := InitializeRedis(ctx, admin, "test", "replacement"); err == nil {
		t.Fatal("replaced original epoch")
	}
	if got := admin.Get(ctx, one.shared.prefix+"epoch").Val(); got != "fixed-original-epoch" {
		t.Fatalf("epoch=%s", got)
	}
	if err := InitializeRedis(ctx, admin, "brand-new", "new-epoch"); err != nil {
		t.Fatal(err)
	}
	fresh, err := OpenRedis(clients(), "brand-new", "new-epoch")
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if err := fresh.Start(ctx, sharedPolicy(), "first-request", time.Now()); err != nil {
		t.Fatal(err)
	}
}
func TestRedisBudgetArchivesDoNotExhaustActiveLedgerCapacity(t *testing.T) {
	one, _, clients := sharedTestStores(t)
	p := sharedPolicy()
	p.LimitNanoUSD = 100000
	p.RequestCapNanoUSD = 100000
	ctx := context.Background()
	if err := one.Start(ctx, p, "initial", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := one.Finalize(ctx, "initial", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	admin := clients()
	defer admin.Close()
	key := one.shared.prefix + "p:" + principalDigest(p.PrincipalID)
	raw, _ := admin.Get(ctx, key).Bytes()
	var state sharedState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Requests) != 0 {
		t.Fatal("settled request retained in active ledger")
	}
	for _, cycle := range state.Cycles {
		cycle.Spent = 10001
	}
	raw, _ = json.Marshal(state)
	admin.Set(ctx, key, raw, 0)
	pipe := admin.Pipeline()
	for i := 0; i < 10001; i++ {
		id := fmt.Sprintf("historical-%05d", i)
		record := sharedRequest{Record: RequestRecord{ID: id, PrincipalID: p.PrincipalID, Period: p.Period, CreatedAt: time.Now().Unix(), Status: "settled", ChargedNanoUSD: 1}, Policy: p, Attempts: map[string]*sharedAttempt{}}
		archived, _ := json.Marshal(record)
		pipe.Set(ctx, one.shared.archiveKey(id), archived, 0)
		pipe.HSet(ctx, one.shared.prefix+"requests", id, principalDigest(p.PrincipalID))
		pipe.ZAdd(ctx, key+":history", redis.Z{Score: float64(record.Record.CreatedAt), Member: id})
		if (i+1)%250 == 0 {
			if _, err := pipe.Exec(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := one.Start(ctx, p, "after-10001-settlements", time.Now()); err != nil {
		t.Fatalf("archived history blocked new request: %v", err)
	}
	if err := one.Reserve(ctx, p, "after-10001-settlements", sharedQuote("after.a1", 100), time.Now()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := one.Snapshot(ctx, p, time.Now())
	if err != nil || snapshot.Spent != 10001 || snapshot.Reserved != 100 {
		t.Fatalf("archive reset totals: %+v %v", snapshot, err)
	}
	if owned, err := one.RequestOwned(ctx, "historical-00000", p.PrincipalID); err != nil || !owned {
		t.Fatalf("archive unreadable owned=%v err=%v", owned, err)
	}
}
func TestRedisBudgetRollingWindowIncludesArchivedCharges(t *testing.T) {
	one, two, _ := sharedTestStores(t)
	ctx := context.Background()
	p := sharedPolicy()
	p.Period = "rolling"
	p.RollingSeconds = 60
	if err := one.Start(ctx, p, "rolling", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := one.Reserve(ctx, p, "rolling", sharedQuote("rolling.a1", 800), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := one.Finalize(ctx, "rolling", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	snap, err := two.Snapshot(ctx, p, time.Now())
	if err != nil || snap.Spent != 800 {
		t.Fatalf("archived rolling usage missing: %+v %v", snap, err)
	}
	if err := two.Start(ctx, p, "rolling-next", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := two.Reserve(ctx, p, "rolling-next", sharedQuote("rolling-next.a1", 201), time.Now()); err != ErrQuota {
		t.Fatalf("rolling overspend=%v", err)
	}

	// Prepare the same settled charge in the past without a minute-long sleep.
	// Only the auxiliary window index ages out; its immutable bill remains.
	key := one.shared.prefix + "p:" + principalDigest(p.PrincipalID)
	past := strconv.FormatInt(time.Now().Unix()-120, 10)
	fields, err := one.shared.client.HGetAll(ctx, key+":rolling").Result()
	if err != nil {
		t.Fatal(err)
	}
	pipe := one.shared.client.Pipeline()
	for second, amount := range fields {
		if second == "schema" {
			continue
		}
		pipe.HDel(ctx, key+":rolling", second)
		pipe.ZRem(ctx, key+":rolling-index", second)
		pipe.HSet(ctx, key+":rolling", past, amount)
		stamp, _ := strconv.ParseInt(past, 10, 64)
		pipe.ZAdd(ctx, key+":rolling-index", redis.Z{Score: float64(stamp), Member: past})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatal(err)
	}
	archiveBefore, err := one.shared.client.Get(ctx, one.shared.archiveKey("rolling")).Result()
	if err != nil {
		t.Fatal(err)
	}
	snap, err = two.Snapshot(ctx, p, time.Now())
	if err != nil || snap.Spent != 0 {
		t.Fatalf("expired rolling bucket still charged: %+v %v", snap, err)
	}
	if err := two.Reserve(ctx, p, "rolling-next", sharedQuote("rolling-next.a1", 801), time.Now()); err != nil {
		t.Fatalf("aged window not reusable: %v", err)
	}
	if archiveAfter := one.shared.client.Get(ctx, one.shared.archiveKey("rolling")).Val(); archiveAfter != archiveBefore {
		t.Fatal("window aging changed or deleted archived bill")
	}
}

func TestRedisBudgetManagerValidatesEpochBeforeAdmission(t *testing.T) {
	_, _, clients := sharedTestStores(t)
	for _, realm := range []struct{ namespace, epoch string }{{"missing", "fixed-original-epoch"}, {"test", "wrong-epoch"}} {
		if manager, err := NewRedisManager(clients(), realm.namespace, realm.epoch); err != ErrUnavailable || manager != nil {
			if manager != nil {
				manager.Close()
			}
			t.Fatalf("invalid realm accepted: %v", err)
		}
	}
}
