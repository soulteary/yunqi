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
