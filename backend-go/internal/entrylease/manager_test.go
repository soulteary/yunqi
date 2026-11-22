package entrylease

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/soulteary/yunqi/internal/requestscope"
)

// Every integration test starts and cleans up its own Unix-socket Redis process.
// It never reads REDIS_URL or connects to an already running service.
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
func TestRedisTwoNodesShareAtomicConcurrency(t *testing.T) {
	newClient := isolatedRedis(t)
	managers := []*Manager{newManager(newClient(), "concurrency", time.Second), newManager(newClient(), "concurrency", time.Second)}
	for _, m := range managers {
		defer m.Close()
	}
	var accepted atomic.Int64
	var mu sync.Mutex
	var leases []*Lease
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lease, _, err := managers[i%2].Acquire(context.Background(), "principal:stable-team", 3, 0)
			if err == nil {
				accepted.Add(1)
				mu.Lock()
				leases = append(leases, lease)
				mu.Unlock()
			} else if !errors.Is(err, ErrConcurrency) {
				t.Errorf("unexpected admission failure: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 3 {
		t.Fatalf("two-node concurrency admitted %d want 3", accepted.Load())
	}
	for _, lease := range leases {
		lease.ParentDone()
		lease.ParentDone()
	}
	lease, _, err := managers[1].Acquire(context.Background(), "principal:stable-team", 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	lease.ParentDone()
	if strings.Contains(managers[0].keys("principal:stable-team")[0], "stable-team") {
		t.Fatal("principal plaintext leaked into Redis keys")
	}
}
func TestRedisRPMReleaseKeepsAcceptedRequestsAndPurgesOldWindow(t *testing.T) {
	newClient := isolatedRedis(t)
	first := newManager(newClient(), "rpm", time.Second)
	second := newManager(newClient(), "rpm", time.Second)
	defer first.Close()
	defer second.Close()
	for _, m := range []*Manager{first, second} {
		lease, _, err := m.Acquire(context.Background(), "team", 0, 2)
		if err != nil {
			t.Fatal(err)
		}
		lease.ParentDone()
	}
	if _, retry, err := first.Acquire(context.Background(), "team", 0, 2); !errors.Is(err, ErrRPM) || retry <= 0 || retry > time.Minute {
		t.Fatalf("RPM was refunded: %v %v", retry, err)
	}
	keys := first.keys("team")
	ctx := context.Background()
	now, err := first.client.Time(ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	members, err := first.client.ZRange(ctx, keys[1], 0, -1).Result()
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if err := first.client.ZAdd(ctx, keys[1], redis.Z{Score: float64(now.Add(-61 * time.Second).UnixMilli()), Member: member}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	lease, _, err := second.Acquire(ctx, "team", 0, 2)
	if err != nil {
		t.Fatal("old rolling window did not expire", err)
	}
	lease.ParentDone()
}
func TestRedisRenewalAndLostNodeLeaseExpiry(t *testing.T) {
	newClient := isolatedRedis(t)
	ttl := 300 * time.Millisecond
	first := newManager(newClient(), "renew", ttl)
	second := newManager(newClient(), "renew", ttl)
	defer first.Close()
	defer second.Close()
	lease, _, err := first.Acquire(context.Background(), "team", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * ttl)
	if _, _, err := second.Acquire(context.Background(), "team", 1, 0); !errors.Is(err, ErrConcurrency) {
		t.Fatalf("live node lease expired: %v", err)
	}
	// Detach client cancellation, retaining the resource source exactly as F49
	// does. Losing Redis must still cancel and close real bound body resources.
	ctx, binding, err := requestscope.Bind(context.WithoutCancel(WithLease(context.Background(), lease)))
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	defer writer.Close()
	body := requestscope.WrapBody(ctx, reader, binding)
	stopped := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, body); close(stopped) }()
	_ = first.client.Close() // Simulate a node losing its Redis connection.
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("lost lease did not cancel detached body")
	}
	if !errors.Is(context.Cause(ctx), ErrLeaseLost) {
		t.Fatalf("wrong cancellation cause %v", context.Cause(ctx))
	}
	lease.ParentDone()
	time.Sleep(ttl)
	recovered, _, err := second.Acquire(context.Background(), "team", 1, 0)
	if err != nil {
		t.Fatalf("orphan lease was not reclaimed: %v", err)
	}
	recovered.ParentDone()
}
func TestRedisAcquireAndReleaseAreIdempotent(t *testing.T) {
	newClient := isolatedRedis(t)
	client := newClient()
	defer client.Close()
	m := newManager(client, "idempotent", time.Second)
	keys := m.keys("team")
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		result, err := acquireScript.Run(ctx, client, keys, "owned-nonce", 1000, 1, 1).Int64Slice()
		if err != nil || len(result) != 2 || result[0] != 1 {
			t.Fatalf("replay %v %v", result, err)
		}
	}
	if got := client.ZCard(ctx, keys[1]).Val(); got != 1 {
		t.Fatalf("duplicate acquisition charged RPM %d times", got)
	}
	for i := 0; i < 2; i++ {
		if _, err := releaseScript.Run(ctx, client, keys[:1], "owned-nonce").Result(); err != nil {
			t.Fatal(err)
		}
	}
	if got := client.ZCard(ctx, keys[0]).Val(); got != 0 {
		t.Fatal("release left an active lease")
	}
}
func TestRedisConfigurationErrorsNeverEchoSecrets(t *testing.T) {
	for _, value := range []string{"http://user:super-secret@redis.invalid", "redis://user:super-secret@redis.invalid?unknown=secret"} {
		if _, err := New(value, "yunqi", 30*time.Second); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe configuration error %v", err)
		}
	}
}

func TestRedisMixedLeaseTTLCannotExpireAnotherNodesLongLease(t *testing.T) {
	newClient := isolatedRedis(t)
	client := newClient()
	defer client.Close()
	manager := newManager(client, "mixed", time.Second)
	keys := manager.keys("team")
	ctx := context.Background()
	for _, lease := range []struct {
		id  string
		ttl int
	}{{"long-node", 30000}, {"short-node", 5000}} {
		if _, err := acquireScript.Run(ctx, client, keys, lease.id, lease.ttl, 2, 0).Result(); err != nil {
			t.Fatal(err)
		}
	}
	assertLongAlive := func(stage string) {
		t.Helper()
		ttl, err := client.PTTL(ctx, keys[0]).Result()
		if err != nil || ttl < 29*time.Second {
			t.Fatalf("%s shortened long node lease: %v %v", stage, ttl, err)
		}
	}
	assertLongAlive("short acquire")
	if _, err := renewScript.Run(ctx, client, keys[:1], "short-node", 5000).Result(); err != nil {
		t.Fatal(err)
	}
	assertLongAlive("short renewal")
	if _, err := releaseScript.Run(ctx, client, keys[:1], "short-node").Result(); err != nil {
		t.Fatal(err)
	}
	assertLongAlive("short release")
	if got := client.ZCard(ctx, keys[0]).Val(); got != 1 {
		t.Fatalf("long node lost from lease set: %d", got)
	}
}

type delayedAcquireReply struct {
	sha   string
	delay time.Duration
}

func (h *delayedAcquireReply) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *delayedAcquireReply) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *delayedAcquireReply) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err == nil && cmd.Name() == "evalsha" && len(cmd.Args()) > 1 && cmd.Args()[1] == h.sha {
			time.Sleep(h.delay)
		}
		return err
	}
}
func TestRedisLocalWatchdogStartsBeforeSlowAcquireReply(t *testing.T) {
	newClient := isolatedRedis(t)
	client := newClient()
	ctx := context.Background()
	if err := acquireScript.Load(ctx, client).Err(); err != nil {
		t.Fatal(err)
	}
	client.AddHook(&delayedAcquireReply{sha: acquireScript.Hash(), delay: 1200 * time.Millisecond})
	manager := newManager(client, "watchdog", 1500*time.Millisecond)
	defer manager.Close()
	started := time.Now()
	lease, _, err := manager.Acquire(ctx, "team", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	bound, binding, err := requestscope.Bind(context.WithoutCancel(WithLease(ctx, lease)))
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	defer writer.Close()
	body := requestscope.WrapBody(bound, reader, binding)
	defer body.Close()
	lease.ParentDone()
	// Renewal is scheduled 500ms after the delayed reply (t=1700ms). The
	// conservative watchdog must cancel around t=1500ms instead of oversubscribing.
	wait := time.Until(started.Add(1620 * time.Millisecond))
	if wait < 0 {
		t.Fatal("test scheduler delayed past watchdog assertion")
	}
	select {
	case <-bound.Done():
	case <-time.After(wait):
		t.Fatal("slow acquire reply extended local lease validity")
	}
	if !errors.Is(requestscope.Cause(bound), ErrLeaseLost) {
		t.Fatalf("watchdog cause %v", requestscope.Cause(bound))
	}
	if lease.refreshDeadline(time.Now().Add(time.Minute)) {
		t.Fatal("late acknowledgement resurrected cancelled lease")
	}
}
