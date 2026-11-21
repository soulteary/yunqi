package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/session"
)

// Start from real blacklist/status operations and reload persisted configuration,
// rather than seeding synthetic disabled-key records. Restoration may replenish
// a key pool, but must not undo an operator's explicit suspension or disable.
func TestDueRecoveryLifecycleAcrossProtocols(t *testing.T) {
	for _, kind := range []ChannelKind{ChannelKindMessages, ChannelKindResponses, ChannelKindChat, ChannelKindGemini, ChannelKindImages, ChannelKindVectors} {
		t.Run(string(kind), func(t *testing.T) {
			policy := config.ChannelKindRegistry[config.ChannelKind(kind)]
			cfg := config.Config{}
			var upstreams []config.UpstreamConfig
			for _, name := range []string{"automatic", "manual", "disabled", "authentication", "model-only"} {
				key := "test-" + name
				upstreams = append(upstreams, config.UpstreamConfig{
					Name: name, BaseURL: fmt.Sprintf("https://%s.example.invalid", name),
					APIKeys: []string{key}, APIKeyConfigs: []config.APIKeyConfig{{Key: key, Name: name, Weight: 3}},
					Status: "active", ServiceType: policy.DefaultServiceType, Priority: len(upstreams) + 1,
				})
			}
			*policy.SliceRef(&cfg) = upstreams
			path := filepath.Join(t.TempDir(), "config.json")
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			cm, err := config.NewConfigManager(path, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cm.Close() })
			now := time.Now().UTC()
			recoverAt := now.Add(time.Minute).Format(time.RFC3339)
			apiType := kindAPIType(kind)
			for index, name := range []string{"automatic", "manual", "disabled", "authentication"} {
				reason := "insufficient_quota"
				if name == "authentication" {
					reason = "authentication_error"
				}
				if err := cm.BlacklistKeyWithRecoverAt(apiType, index, "test-"+name, reason, "test limit", recoverAt); err != nil {
					t.Fatal(err)
				}
			}
			for index, status := range map[int]string{1: "suspended", 2: "disabled"} {
				if _, err := config.UpdateUpstreamByKind(cm, config.ChannelLocation{Kind: policy.Kind, Index: index}, config.UpstreamUpdate{Status: &status}); err != nil {
					t.Fatal(err)
				}
			}
			if err := cm.DisableKeyModel(apiType, 4, "test-model-only", "test-model", "insufficient_quota", "test model limit", recoverAt); err != nil {
				t.Fatal(err)
			}
			// Loading a channel with no usable keys creates the auto_no_keys pause.
			if err := cm.Close(); err != nil {
				t.Fatal(err)
			}
			cm, err = config.NewConfigManager(path, "")
			if err != nil {
				t.Fatal(err)
			}
			before := policy.Slice(cm.GetConfig())
			if before[0].Status != "suspended" || before[0].SuspensionSource != config.SuspensionSourceAutoNoKeys {
				t.Fatalf("quota-only channel did not acquire automatic no-key pause: %+v", before[0])
			}
			if before[1].SuspensionSource != config.SuspensionSourceManual {
				t.Fatal("manual pause source was lost")
			}
			managers := make([]*metrics.MetricsManager, 6)
			for i := range managers {
				managers[i] = metrics.NewMetricsManager()
				t.Cleanup(managers[i].Stop)
			}
			affinity := session.NewTraceAffinityManager()
			t.Cleanup(affinity.Stop)
			s := NewChannelScheduler(cm, managers[0], managers[1], managers[2], managers[3], managers[4], affinity, nil, managers[5])
			t.Cleanup(s.Stop)
			if got, err := s.RunDueRecoveries(now); err != nil || len(got) != 0 {
				t.Fatalf("recovered before due time: %+v, err=%v", got, err)
			}
			results, err := s.RunDueRecoveries(now.Add(2 * time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			after := policy.Slice(cm.GetConfig())
			if after[1].Status != "suspended" || after[1].SuspensionSource != config.SuspensionSourceManual {
				t.Errorf("automatic key restoration undid manual channel pause: status=%q source=%q", after[1].Status, after[1].SuspensionSource)
			}
			if after[0].Status != "active" || after[0].SuspensionSource != "" || len(after[0].APIKeys) != 1 || after[0].APIKeys[0] != "test-automatic" {
				t.Errorf("automatic pause did not recover: status=%q source=%q keys=%v", after[0].Status, after[0].SuspensionSource, after[0].APIKeys)
			}
			if len(after[0].APIKeyConfigs) != 1 || after[0].APIKeyConfigs[0].Weight != 3 {
				t.Error("restoration lost key configuration")
			}
			if len(after[1].APIKeys) != 1 || len(after[1].DisabledAPIKeys) != 0 {
				t.Error("manual pause should retain restored keys for later operator activation")
			}
			for _, index := range []int{2, 3} {
				if len(after[index].APIKeys) != 0 || len(after[index].DisabledAPIKeys) != 1 {
					t.Errorf("restored protected channel %d", index)
				}
			}
			if after[2].Status != "disabled" || after[3].Status != "suspended" || after[3].DisabledAPIKeys[0].RecoverAt != "" {
				t.Error("operator disable or authentication ban was bypassed")
			}
			if len(after[4].DisabledKeyModels) != 0 || after[4].Status != "active" || len(after[4].APIKeys) != 1 {
				t.Error("model-only expiry changed the key or channel state")
			}
			activated := 0
			for _, result := range results {
				if result.ActivatedChannel {
					activated++
				}
			}
			if len(results) != 3 || activated != 1 {
				t.Errorf("recovery results=%+v, want 3 changed routes with only automatic route activated", results)
			}
			manager := s.getMetricsManager(kind)
			serviceType := NormalizedMetricsServiceType(kind, after[0].ServiceType)
			if got := manager.GetKeyCircuitState(after[0].BaseURL, "test-automatic", serviceType); got != metrics.CircuitStateHalfOpen {
				t.Errorf("restored key state=%v, want half-open", got)
			}
			if !manager.TryAcquireProbe(after[0].BaseURL, "test-automatic", serviceType) {
				t.Fatal("restored key cannot be probed")
			}
			if manager.TryAcquireProbe(after[0].BaseURL, "test-automatic", serviceType) {
				t.Error("restored key allowed concurrent half-open probes")
			}
			manager.ReleaseProbe(after[0].BaseURL, "test-automatic", serviceType)
			persisted, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// Disk uses authoritative ChannelsV3, so verify through the real loader.
			persistedPath := filepath.Join(t.TempDir(), "reloaded.json")
			if err := os.WriteFile(persistedPath, persisted, 0600); err != nil {
				t.Fatal(err)
			}
			reloaded, err := config.NewConfigManager(persistedPath, "")
			if err != nil {
				t.Fatal(err)
			}
			disk := reloaded.GetConfig()
			_ = reloaded.Close()
			if got := policy.Slice(disk); len(got) != 5 || got[0].Status != "active" || got[1].Status != "suspended" || got[2].Status != "disabled" {
				t.Error("recovery state was not durably saved")
			}
			if again, err := s.RunDueRecoveries(now.Add(3 * time.Minute)); err != nil || len(again) != 0 {
				t.Errorf("recovery was not idempotent: %+v err=%v", again, err)
			}
		})
	}
}
