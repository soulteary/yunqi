package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/metrics"
)

func newMetricsPolicyTestApplication(t *testing.T, body string) *application {
	t.Helper()
	t.Setenv("METRICS_PERSISTENCE_ENABLED", "false")
	t.Setenv("METRICS_WINDOW_SIZE", "50")
	t.Setenv("METRICS_FAILURE_THRESHOLD", "1")
	t.Setenv("CIRCUIT_BREAKER_COOLDOWN", "5") // Unsupported legacy-looking variable must not change the actual policy.
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cm, err := config.NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	previousRequest := config.GetRuntimeRequestTimeoutMs(300000)
	previousHeader := config.GetRuntimeResponseHeaderTimeoutMs(120000)
	a := &application{cfgManager: cm, envCfg: config.NewEnvConfig()}
	a.initMetrics()
	t.Cleanup(func() {
		_ = cm.Close() // Stop watching before restoring process-global timeout defaults.
		a.traceAffinityManager.Stop()
		for _, manager := range metricsPolicyManagers(a) {
			manager.Stop()
		}
		config.SetRuntimeTimeouts(previousRequest, previousHeader)
	})
	return a
}

func metricsPolicyManagers(a *application) map[string]*metrics.MetricsManager {
	return map[string]*metrics.MetricsManager{
		"messages": a.messagesMetricsManager, "responses": a.responsesMetricsManager,
		"chat": a.chatMetricsManager, "gemini": a.geminiMetricsManager,
		"images": a.imagesMetricsManager, "vectors": a.vectorsMetricsManager,
	}
}

func TestMetricsStartupThresholdOneRetainsConsecutiveFailureProtection(t *testing.T) {
	a := newMetricsPolicyTestApplication(t, `{}`)
	for kind, manager := range metricsPolicyManagers(a) {
		t.Run(kind, func(t *testing.T) {
			params := manager.GetCircuitBreakerConfig()
			if params.WindowSize != 50 || params.FailureThreshold != 1 || params.ConsecutiveFailuresThreshold != 5 {
				t.Fatalf("unexpected environment fallback policy: %+v", params)
			}
			const baseURL, key = "https://example.invalid", "test-key"
			// Two models exercise Key-wide protection rather than the single-model isolation exception.
			for i := 0; i < 5; i++ {
				model := []string{"model-a", "model-b"}[i%2]
				requestID := manager.RecordRequestConnected(baseURL, key, "openai", model)
				manager.RecordRequestFinalizeFailureWithClass(baseURL, key, "openai", requestID, metrics.FailureClassRetryable)
				want := metrics.CircuitStateClosed
				if i == 4 {
					want = metrics.CircuitStateOpen
				}
				if got := manager.GetKeyCircuitState(baseURL, key, "openai"); got != want {
					t.Fatalf("after %d failures state=%v, want %v", i+1, got, want)
				}
			}
			state := manager.GetKeyMetrics(baseURL, key, "openai")
			if state.CircuitBrokenAt == nil || state.NextRetryAt == nil {
				t.Fatal("missing circuit retry timestamps")
			}
			if backoff := state.NextRetryAt.Sub(*state.CircuitBrokenAt); backoff != 30*time.Second {
				t.Fatalf("unsupported CIRCUIT_BREAKER_COOLDOWN changed backoff: %v", backoff)
			}
		})
	}
}

func TestMetricsPersistedPolicyOverridesEnvironmentAndHotUpdatesAllProtocols(t *testing.T) {
	a := newMetricsPolicyTestApplication(t, `{"circuitBreaker":{"windowSize":12,"failureThreshold":0.85,"consecutiveFailuresThreshold":10}}`)
	check := func(wantThreshold float64) {
		t.Helper()
		for kind, manager := range metricsPolicyManagers(a) {
			got := manager.GetCircuitBreakerConfig()
			if got.WindowSize != 12 || got.FailureThreshold != wantThreshold || got.ConsecutiveFailuresThreshold != 10 {
				t.Errorf("%s runtime policy=%+v, want window=12 threshold=%v consecutive=10", kind, got, wantThreshold)
			}
		}
	}
	check(0.85)
	threshold := 0.95
	applied := make(chan struct{}, 1)
	// Config change callbacks run asynchronously; observe the callback after initMetrics.
	a.cfgManager.RegisterOnConfigChange(func(cfg config.Config) {
		if cfg.CircuitBreaker != nil && cfg.CircuitBreaker.FailureThreshold != nil && *cfg.CircuitBreaker.FailureThreshold == threshold {
			select {
			case applied <- struct{}{}:
			default:
			}
		}
	})
	if err := a.cfgManager.SetCircuitBreakerConfig(config.CircuitBreakerConfig{FailureThreshold: &threshold}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-applied:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime circuit policy callback did not apply")
	}
	check(0.95) // Partial updates retain persisted fields and the callback reaches every protocol.
}
