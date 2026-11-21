package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/guardrails"
	"github.com/soulteary/yunqi/internal/redaction"
)

func TestStartupObservesUnknownCredentialBeforeInitialTypedConfigFailure(t *testing.T) {
	const identitySecret = "opaque!identity-proxy-secret=private"
	t.Setenv("IDENTITY_PROXY_SECRET", identitySecret)
	restore := initializeCredentialRedaction(&config.EnvConfig{ProxyAccessKey: "gateway-for-startup-test"})
	defer restore()
	if redaction.String(identitySecret) != redaction.Mask {
		t.Fatal("identity proxy secret was not protected before initial config load")
	}
	const candidate = "opaque!initial-load-candidate=private"
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"upstream":[{"apiKeys":["`+candidate+`"],"baseUrl":"https://example.test"}],"circuitBreaker":{"windowSize":"invalid"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	previous := log.Writer()
	var output bytes.Buffer
	log.SetOutput(redaction.Writer(&output))
	defer log.SetOutput(previous)
	cm, err := config.NewConfigManager(path, "")
	if cm != nil {
		_ = cm.Close()
	}
	if err == nil {
		t.Fatal("expected first typed config decode failure")
	}
	// This is the startup error boundary, including diagnostics that may echo a
	// rejected candidate that never reached a successful ConfigManager snapshot.
	log.Printf("[Startup] candidate %s rejected: %v", candidate, err)
	if strings.Contains(output.String(), candidate) || !strings.Contains(output.String(), redaction.Mask) {
		t.Fatalf("initial-load credential was not protected: %s", output.String())
	}
}

func TestGuardrailSetupDoesNotClearStartupCapacityFailClosedState(t *testing.T) {
	env := &config.EnvConfig{ProxyAccessKey: "gateway-for-capacity-startup"}
	restore := initializeCredentialRedaction(env)
	defer restore()
	redaction.Add([]string{strings.Repeat("x", redaction.MaxSingleCredentialBytes+1)})
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	cm, err := config.NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	old := guardrails.DefaultRegistry().List()
	defer func() {
		guardrails.ResetDefaultRegistry()
		for _, g := range old {
			guardrails.DefaultRegistry().Register(g)
		}
	}()
	a := &application{envCfg: env, cfgManager: cm}
	a.configureGuardrails()
	if !redaction.RegistryStats().FailClosed {
		t.Fatal("later startup stage reset capacity protection")
	}
	if safe := redaction.String("unknown new secret"); safe != redaction.CapacityMask {
		t.Fatalf("later startup leaked diagnostic: %q", safe)
	}
}

func TestStartupObservesProgrammaticIdentityProxySecret(t *testing.T) {
	t.Setenv("IDENTITY_PROXY_SECRET", "")
	secret := "programmatic-only!identity=private-unique"
	restore := initializeCredentialRedaction(&config.EnvConfig{IdentityProxySecret: secret})
	defer restore()
	if redaction.String(secret) != redaction.Mask {
		t.Fatal("programmatic identity secret was not registered before config loading")
	}
}
