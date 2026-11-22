package redisconn

import (
	"context"
	"github.com/redis/go-redis/v9"
	"strings"
	"testing"
	"time"
)

func TestRedisClientRejectsMetadataWithoutDialOrCredentialEcho(t *testing.T) {
	client, err := New("redis://user:private-password@169.254.169.254:6379")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = client.Ping(ctx).Err()
	if err == nil || !strings.Contains(err.Error(), "metadata") || strings.Contains(err.Error(), "private-password") {
		t.Fatalf("unsafe policy failure %v", err)
	}
}
func TestRedisClientPolicyHookChecksPooledOperations(t *testing.T) {
	t.Setenv("YUNQI_OUTBOUND_BLOCK_PRIVATE", "")
	t.Setenv("YUNQI_OUTBOUND_ALLOWED_HOSTS", "")
	t.Setenv("YUNQI_OUTBOUND_ALLOWED_PORTS", "")
	called := false
	hook := &policyHook{network: "tcp", address: "127.0.0.1:6379"}
	process := hook.ProcessHook(func(context.Context, redis.Cmder) error { called = true; return nil })
	if err := process(context.Background(), nil); err != nil || !called {
		t.Fatal("default private policy changed", err)
	}
	called = false
	t.Setenv("YUNQI_OUTBOUND_BLOCK_PRIVATE", "true")
	if err := process(context.Background(), nil); err == nil || called {
		t.Fatal("tightened policy bypassed on pooled operation")
	}
}
func TestRedisClientConfigurationNeverEchoesPassword(t *testing.T) {
	if _, err := New("redis://user:private-password@host:bad"); err == nil || strings.Contains(err.Error(), "private-password") {
		t.Fatalf("unsafe parse failure %v", err)
	}
}
