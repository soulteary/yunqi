package scheduler

import (
	"context"
	"errors"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/session"
	"sync"
	"testing"
)

func TestStrictSelectionConcurrentFirstRequestsAndFailClosed(t *testing.T) {
	s, m := newTestSchedulerWithMessagesChannels(t, []config.UpstreamConfig{{Name: "first", ChannelUID: "a", BaseURL: "https://a.invalid", APIKeys: []string{"a-key"}, Status: "active", Priority: 1}, {Name: "second", ChannelUID: "b", BaseURL: "https://b.invalid", APIKeys: []string{"b-key"}, Status: "active", Priority: 2}})
	defer m.Stop()
	s.messagesMetricsManager = metrics.NewMetricsManager()
	defer s.messagesMetricsManager.Stop()
	secondName := s.configManager.GetConfig().Upstream[1].Name
	var wg sync.WaitGroup
	start := make(chan struct{})
	routes := make(chan ChannelRouteRef, 32)
	for i := 0; i < 32; i++ {
		l, e := m.AcquireStrict("principal-session")
		if e != nil {
			t.Fatal(e)
		}
		wg.Add(1)
		go func(l *session.StrictLease) {
			defer wg.Done()
			defer l.Release()
			<-start
			r, e := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, Model: "m", StrictSession: l})
			if e != nil {
				t.Error(e)
				return
			}
			routes <- r.Route
		}(l)
	}
	close(start)
	wg.Wait()
	close(routes)
	var first ChannelRouteRef
	for r := range routes {
		if first.ChannelUID == "" {
			first = r
		}
		if first.ChannelUID != r.ChannelUID {
			t.Fatal("first requests split", first, r)
		}
	}
	if first.ChannelUID == "" {
		t.Fatal("no route bound")
	}
	lease, _ := m.AcquireStrict("principal-session")
	defer lease.Release()
	// Failed primary, caller pin and entry-policy scope cannot migrate the session.
	cases := []SelectionOptions{{Kind: ChannelKindMessages, Model: "m", FailedRoutes: map[ChannelRouteKey]bool{first.Key(): true}}, {Kind: ChannelKindMessages, Model: "m", ChannelName: secondName}, {Kind: ChannelKindMessages, Model: "m", CandidateFilter: func(ch []ChannelInfo, _ func(ChannelInfo) *config.UpstreamConfig, _ func(ChannelInfo, *config.UpstreamConfig) bool) ([]ChannelInfo, error) {
		return nil, nil
	}}}
	for _, opts := range cases {
		opts.StrictSession = lease
		if r, e := s.SelectChannelWithOptions(context.Background(), opts); !errors.Is(e, session.ErrStrictSessionUnavailable) || r != nil {
			t.Fatalf("bound route bypassed: %+v %v", r, e)
		}
	}
	// A separate session can still select the other allowed resource.
	other, _ := m.AcquireStrict("other-session")
	defer other.Release()
	r, e := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, ChannelName: secondName, StrictSession: other})
	if e != nil || r.Route.ChannelUID == first.ChannelUID {
		t.Fatalf("separate session blocked: %+v %v", r, e)
	}
	stale, _ := m.AcquireStrict("stale-index")
	defer stale.Release()
	old := first
	old.Index = 999
	_, _ = stale.Bind(old)
	actual, e := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, Model: "m", StrictSession: stale})
	if e != nil || actual.Route.Index == 999 || actual.Route.ChannelUID != first.ChannelUID {
		t.Fatalf("strict binding used list index: %+v %v", actual, e)
	}
	disabled := "disabled"
	if _, e := s.configManager.UpdateUpstream(0, config.UpstreamUpdate{Status: &disabled}); e != nil {
		t.Fatal(e)
	}
	if actual, e := s.SelectChannelWithOptions(context.Background(), SelectionOptions{Kind: ChannelKindMessages, Model: "m", StrictSession: lease}); e == nil || actual != nil {
		t.Fatal("disabled bound resource silently migrated", actual, e)
	}

}
