package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/redaction"
)

func TestRedisCredentialIsRegisteredBeforeConfigurationLogs(t *testing.T) {
	password := "synthetic-redis-secret"
	raw := "redis://user:" + password + "@redis.invalid:6379/0"
	env := &config.EnvConfig{ProxyRedisURL: raw}
	cleanup := initializeCredentialRedaction(env)
	defer cleanup()
	if got := redaction.String("connection " + raw + " password=" + password); strings.Contains(got, password) {
		t.Fatalf("Redis credential not redacted: %s", got)
	}
	encoded, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), password) || strings.Contains(string(encoded), raw) {
		t.Fatal("Redis connection URL leaked through environment JSON")
	}
}

func TestSharedIdentityRedisCredentialIsProtectedBeforeConfigurationLogs(t *testing.T) {
	password := "synthetic-identity-redis-password+decoded"
	raw := "redis://user:synthetic-identity-redis-password%2Bdecoded@redis.invalid:6379/0"
	env := &config.EnvConfig{IdentityProxyRevocationRedisURL: raw}
	cleanup := initializeCredentialRedaction(env)
	defer cleanup()
	safe := redaction.String("connection " + raw + " password=" + password)
	if strings.Contains(safe, raw) || strings.Contains(safe, password) {
		t.Fatal("identity revocation credential not protected")
	}
	encoded, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "synthetic-identity-redis-password") {
		t.Fatal("identity revocation URL leaked through environment JSON")
	}
}
