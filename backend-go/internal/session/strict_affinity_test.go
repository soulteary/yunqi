package session

import (
	"fmt"
	"github.com/soulteary/yunqi/internal/routingref"
	"sync"
	"testing"
	"time"
)

func TestStrictFirstBindIsAtomicAndActiveLeaseCannotExpire(t *testing.T) {
	m := NewTraceAffinityManagerWithTTL(time.Minute)
	defer m.Stop()
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan routingref.RouteRef, 64)
	leases := make([]*StrictLease, 64)
	for i := range leases {
		l, e := m.AcquireStrict("session")
		if e != nil {
			t.Fatal(e)
		}
		leases[i] = l
		wg.Add(1)
		go func(i int, l *StrictLease) {
			defer wg.Done()
			<-start
			r, e := l.Bind(routingref.RouteRef{Kind: "chat", ChannelUID: fmt.Sprintf("ch%d", i)})
			if e != nil {
				t.Error(e)
			}
			results <- r
		}(i, l)
	}
	close(start)
	wg.Wait()
	close(results)
	var winner routingref.RouteRef
	for r := range results {
		if winner.ChannelUID == "" {
			winner = r
		}
		if r != winner {
			t.Fatal("split binding", r, winner)
		}
	}
	m.mu.Lock()
	m.strict["session"].lastUsed = time.Now().Add(-time.Hour)
	m.mu.Unlock()
	m.Cleanup()
	later, e := m.AcquireStrict("session")
	if e != nil {
		t.Fatal(e)
	}
	if r, _ := later.Route(); r != winner {
		t.Fatal("in-flight entry expired")
	}
	later.Release()
	for _, l := range leases {
		l.Release()
		l.Release()
	}
	m.mu.Lock()
	m.strict["session"].lastUsed = time.Now().Add(-time.Hour)
	m.mu.Unlock()
	m.Cleanup()
	fresh, _ := m.AcquireStrict("session")
	defer fresh.Release()
	if _, bound := fresh.Route(); bound {
		t.Fatal("idle expired entry stayed bound")
	}
}
func TestStrictCapacityAndOrdinaryAffinityRemainSeparate(t *testing.T) {
	m := NewTraceAffinityManager()
	defer m.Stop()
	for i := 0; i < MaxStrictSessions; i++ {
		l, e := m.AcquireStrict(fmt.Sprint(i))
		if e != nil {
			t.Fatal(e)
		}
		l.Release()
	}
	if _, e := m.AcquireStrict("extra"); e != ErrStrictSessionCapacity {
		t.Fatal(e)
	}
	m.SetPreferredChannel("ordinary", 1)
	m.SetPreferredChannel("ordinary", 2)
	if i, _ := m.GetPreferredChannel("ordinary"); i != 2 {
		t.Fatal("normal affinity frozen")
	}
}
