package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
)

func TestSchedulerProviderUpdateWhileSelectionCallbackWaits(t *testing.T) {
	s, cleanup := createTestScheduler(t, federationTestConfig())
	defer cleanup()
	entered, release := make(chan struct{}), make(chan struct{})
	s.SetCandidateFilterProvider(func(context.Context, ChannelKind, string) (CandidateFilterFunc, CandidateSelectionObserver) {
		close(entered)
		<-release
		return nil, nil
	})
	selected := make(chan error, 1)
	go func() {
		_, err := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, Model: "audit"})
		selected <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("selection never entered provider")
	}
	updated := make(chan struct{})
	go func() { s.SetModelSupportResolverProvider(newFederationResolver()); close(updated) }()
	select {
	case <-updated:
	case <-time.After(time.Second):
		t.Error("provider setter blocked behind a running callback")
	}
	close(release)
	select {
	case err := <-selected:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("selection deadlocked after provider update")
	}
}

func TestSchedulerCallbacksCanUpdateProviders(t *testing.T) {
	for _, stage := range []string{"candidate_provider", "smart_filter", "observer", "model_resolver", "context_resolver", "overflow_provider"} {
		t.Run(stage, func(t *testing.T) {
			cfg := federationTestConfig()
			cfg.Upstream[0].ModelCapabilities = map[string]config.UpstreamModelCapability{"audit": {ContextWindowTokens: 1000}}
			s, cleanup := createTestScheduler(t, cfg)
			defer cleanup()
			called := make(chan struct{}, 1)
			update := func() {
				s.SetQuotaManager(nil)
				select {
				case called <- struct{}{}:
				default:
				}
			}
			opts := SelectionOptions{Kind: ChannelKindMessages, Model: "audit"}
			switch stage {
			case "candidate_provider", "smart_filter", "observer":
				s.SetCandidateFilterProvider(func(context.Context, ChannelKind, string) (CandidateFilterFunc, CandidateSelectionObserver) {
					if stage == "candidate_provider" {
						update()
					}
					return func(channels []ChannelInfo, _ func(ChannelInfo) *config.UpstreamConfig, _ func(ChannelInfo, *config.UpstreamConfig) bool) ([]ChannelInfo, error) {
						if stage == "smart_filter" {
							update()
						}
						return channels, nil
					}, func(string) string {
						if stage == "observer" {
							update()
						}
						return "audit-trace"
					}
				})
			case "model_resolver":
				s.SetModelSupportResolverProvider(func(_ context.Context, _ ChannelKind, _ *config.UpstreamConfig, model string) (bool, string, string, string) {
					update()
					return true, model, "test", ""
				})
			case "context_resolver":
				opts.ContextRequirement = &ContextRequirement{InputTokens: 100, RequiredTokens: 200}
				s.SetContextWindowResolverProvider(func(_ string, _ ChannelKind, _ string, window int) (int, int) { update(); return window, 0 })
			case "overflow_provider":
				opts.ContextRequirement = &ContextRequirement{InputTokens: 500000, RequiredTokens: 508192}
				s.SetOverflowCandidateProvider(func(context.Context, ChannelKind, string, int) []ChannelInfo {
					update()
					u := s.configManager.GetConfig().ChatUpstream[0]
					return []ChannelInfo{{Route: channelRouteRef(ChannelKindChat, 0, &u), Index: 0, Name: u.Name, Status: "active", ActualModel: "kimi-k3", OverflowRedirect: true}}
				})
			}
			done := make(chan error, 1)
			go func() { _, err := s.SelectChannelWithOptions(context.Background(), opts); done <- err }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("callback could not update provider")
			}
			select {
			case <-called:
			default:
				t.Fatal("expected callback was never exercised")
			}
		})
	}
}

func TestSchedulerConcurrentProviderRegistrationAndSelection(t *testing.T) {
	cfg := federationTestConfig()
	cfg.Upstream[0].ModelCapabilities = map[string]config.UpstreamModelCapability{"audit": {ContextWindowTokens: 1000000}}
	s, cleanup := createTestScheduler(t, cfg)
	defer cleanup()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.SetCandidateFilterProvider(nil)
			s.SetModelSupportResolverProvider(newFederationResolver())
			s.SetContextWindowResolverProvider(func(_ string, _ ChannelKind, _ string, w int) (int, int) { return w, 0 })
			s.SetOverflowCandidateProvider(nil)
			s.SetQuotaManager(nil)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_, err := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, Model: "audit", SmartFilter: pinKindFilter(ChannelKindChat), ContextRequirement: &ContextRequirement{InputTokens: 100, RequiredTokens: 200}})
			if err != nil {
				t.Errorf("selection failed: %v", err)
				return
			}
		}
	}()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent provider updates stalled selection")
	}
}
