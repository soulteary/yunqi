package session

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/soulteary/yunqi/internal/entrylease"
	"github.com/soulteary/yunqi/internal/routingref"
	"sync"
	"time"
)

const MaxStrictSessions = 4096

var ErrStrictSessionCapacity = errors.New("strict session capacity reached")
var ErrStrictSessionUnavailable = errors.New("strict session bound route is unavailable")

type strictBinding struct {
	route    routingref.RouteRef
	bound    bool
	refs     int
	lastUsed time.Time
}

// StrictLease keeps an entry alive during requests. Failures never migrate it.
type StrictLease struct {
	manager     *TraceAffinityManager
	entry       *strictBinding
	once        sync.Once
	remote      *entrylease.Lease
	remoteRoute routingref.RouteRef
	remoteMu    sync.Mutex
}

func (m *TraceAffinityManager) AcquireStrict(key string) (*StrictLease, error) {
	if m == nil || key == "" || len(key) > 128 {
		return nil, ErrStrictSessionUnavailable
	}
	m.mu.RLock()
	shared := m.sharedStrict
	m.mu.RUnlock()
	if shared != nil {
		return m.acquireSharedStrict(key, shared)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if m.strict == nil {
		m.strict = make(map[string]*strictBinding)
	}
	entry := m.strict[key]
	if entry != nil && entry.refs == 0 && now.Sub(entry.lastUsed) > m.ttl {
		delete(m.strict, key)
		entry = nil
	}
	if entry == nil {
		if len(m.strict) >= MaxStrictSessions {
			m.cleanupStrictLocked(now)
		}
		if len(m.strict) >= MaxStrictSessions {
			return nil, ErrStrictSessionCapacity
		}
		entry = &strictBinding{lastUsed: now}
		m.strict[key] = entry
	}
	entry.refs++
	entry.lastUsed = now
	return &StrictLease{manager: m, entry: entry}, nil
}
func (l *StrictLease) Route() (routingref.RouteRef, bool) {
	if l == nil {
		return routingref.RouteRef{}, false
	}
	if l.remote != nil {
		l.remoteMu.Lock()
		defer l.remoteMu.Unlock()
		return l.remoteRoute, l.remoteRoute.ChannelUID != ""
	}
	l.manager.mu.RLock()
	defer l.manager.mu.RUnlock()
	return l.entry.route, l.entry.bound
}
func (l *StrictLease) Bind(route routingref.RouteRef) (routingref.RouteRef, error) {
	if l == nil || route.ChannelUID == "" || route.Kind == "" {
		return routingref.RouteRef{}, ErrStrictSessionUnavailable
	}
	if l.remote != nil {
		l.remoteMu.Lock()
		defer l.remoteMu.Unlock()
		encoded, _ := json.Marshal(route)
		winner, err := l.remote.StrictBinding(context.Background(), string(encoded), l.manager.ttl)
		if err != nil {
			return routingref.RouteRef{}, ErrStrictSessionUnavailable
		}
		var bound routingref.RouteRef
		if json.Unmarshal([]byte(winner), &bound) != nil || bound.ChannelUID == "" || bound.Kind == "" {
			return routingref.RouteRef{}, ErrStrictSessionUnavailable
		}
		l.remoteRoute = bound
		return bound, nil
	}
	l.manager.mu.Lock()
	defer l.manager.mu.Unlock()
	if !l.entry.bound {
		l.entry.route = route
		l.entry.bound = true
	}
	l.entry.lastUsed = time.Now()
	return l.entry.route, nil
}
func (l *StrictLease) Release() {
	if l == nil {
		return
	}
	if l.remote != nil {
		l.once.Do(l.remote.ParentDone)
		return
	}
	l.once.Do(func() { l.manager.mu.Lock(); l.entry.refs--; l.entry.lastUsed = time.Now(); l.manager.mu.Unlock() })
}
func (m *TraceAffinityManager) cleanupStrictLocked(now time.Time) int {
	n := 0
	for k, e := range m.strict {
		if e.refs == 0 && now.Sub(e.lastUsed) > m.ttl {
			delete(m.strict, k)
			n++
		}
	}
	return n
}

// SetSharedStrict must be called during startup, before serving requests. The
// caller owns and closes the shared manager after all handlers are drained.
func (m *TraceAffinityManager) SetSharedStrict(shared *entrylease.Manager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sharedStrict = shared
}
func (m *TraceAffinityManager) acquireSharedStrict(key string, shared *entrylease.Manager) (*StrictLease, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	remote, _, err := shared.Acquire(ctx, "strict:"+key, 0, 0)
	if err != nil {
		return nil, ErrStrictSessionUnavailable
	}
	value, err := remote.StrictBinding(ctx, "", m.ttl)
	if err != nil {
		remote.ParentDone()
		return nil, ErrStrictSessionUnavailable
	}
	lease := &StrictLease{manager: m, remote: remote}
	if value != "" && (json.Unmarshal([]byte(value), &lease.remoteRoute) != nil || lease.remoteRoute.ChannelUID == "" || lease.remoteRoute.Kind == "") {
		remote.ParentDone()
		return nil, ErrStrictSessionUnavailable
	}
	return lease, nil
}

// Context binds shared-affinity loss to real upstream body lifetimes, including
// detached usage drain. Local strict sessions preserve the caller's context.
func (l *StrictLease) Context(ctx context.Context) context.Context {
	if l != nil && l.remote != nil {
		return entrylease.WithLease(ctx, l.remote)
	}
	return ctx
}
