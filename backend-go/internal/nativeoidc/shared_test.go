package nativeoidc

import (
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
	"github.com/soulteary/yunqi/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

type testRevocations struct {
	mu      sync.Mutex
	lost    bool
	revoked map[string]bool
}

func (b *testRevocations) Prepare(_ context.Context, epoch string, _ bool) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.lost || epoch == "" {
		return "", errors.New("lost epoch")
	}
	return epoch, nil
}
func (b *testRevocations) Sync(context.Context, []config.IdentitySessionRevocation) error { return nil }
func (b *testRevocations) IsRevoked(_ context.Context, key string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.revoked[key], nil
}
func (b *testRevocations) Revoke(_ context.Context, key string, _ int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.revoked[key] = true
	return nil
}
func (b *testRevocations) Close() error { return nil }
func TestOIDCSharedFlowSessionRevocationAndLostRealm(t *testing.T) {
	newClient := isolatedRedis(t)
	first, idp, cfg := fakeProvider(t)
	backend := &testRevocations{revoked: make(map[string]bool)}
	cfg.RevocationMode = "shared"
	cfg.RevocationEpoch = strings.Repeat("a", 64)
	first.env.IdentityProxyRevocations = backend
	first.env.IdentityProxyRevocationRedisURL = "redis://127.0.0.1:1"
	firstShared, err := New(first.env)
	if err != nil {
		t.Fatal(err)
	}
	_ = firstShared.store.redis.Close()
	firstShared.store.redis = newClient()
	firstShared.client = idp.server.Client()
	first = firstShared
	defer first.Close()
	second, err := New(first.env)
	if err != nil {
		t.Fatal(err)
	}
	_ = second.store.redis.Close()
	second.store.redis = newClient()
	second.client = idp.server.Client()
	defer second.Close()
	// Same configured issuer/client/redirect must share the exact store prefix.
	if second.store.prefix != first.store.prefix {
		t.Fatal("node realm prefixes differ")
	}
	state, browser := startFlow(t, first, idp)
	cookie, err := second.Complete(context.Background(), state, "code", browser)
	if err != nil {
		t.Fatal(err)
	}
	session, err := first.Lookup(context.Background(), cookie)
	if err != nil {
		t.Fatal("node A could not authenticate node B session", err)
	}
	if err = backend.Revoke(context.Background(), session.Fingerprint, session.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err = first.Lookup(context.Background(), cookie); err == nil {
		t.Fatal("shared revoke did not reject on node A")
	}
	if _, err = second.Lookup(context.Background(), cookie); err == nil {
		t.Fatal("shared revoke did not reject on node B")
	}
	state, browser = startFlow(t, first, idp)
	cookie, err = first.Complete(context.Background(), state, "other-code", browser)
	if err != nil {
		t.Fatal(err)
	}
	backend.lost = true
	if _, err = second.Lookup(context.Background(), cookie); !IsUnavailable(err) {
		t.Fatal("missing revocation realm did not fail closed", err)
	}
	backend.lost = false
	cfg.RevocationMode = "local"
	if _, err = first.Lookup(context.Background(), cookie); !IsUnavailable(err) {
		t.Fatal("mode change silently downgraded shared session", err)
	}
	cfg.RevocationMode = "shared"
	_ = second.store.redis.Close()
	if _, err = second.Lookup(context.Background(), cookie); !IsUnavailable(err) {
		t.Fatal("store outage did not fail closed", err)
	}
}

func TestOIDCSharedSessionCapacityAndIssuanceAreAtomic(t *testing.T) {
	newClient := isolatedRedis(t)
	first := &storage{redis: newClient(), prefix: "oidc-capacity:"}
	defer first.close()
	second := &storage{redis: newClient(), prefix: "oidc-capacity:"}
	defer second.close()
	ctx := context.Background()
	expires := time.Now().Add(time.Hour).Unix()
	for i := 0; i < 100; i++ {
		store := first
		if i%2 == 1 {
			store = second
		}
		if err := store.put(ctx, "session:"+strconv.Itoa(i), "value", "subject", expires); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := first.get(ctx, "session:0", "subject", false); err == nil {
		t.Fatal("oldest session survived shared capacity limit")
	}
	if _, err := second.get(ctx, "session:99", "subject", false); err != nil {
		t.Fatal("newest shared login blocked", err)
	}
	if err := first.put(ctx, "excess", "value", "subject", expires); err == nil {
		t.Fatal("shared issuance limit ignored")
	}
	if _, err := second.get(ctx, "session:99", "subject", false); err != nil {
		t.Fatal("rejected shared login evicted live session", err)
	}
}
