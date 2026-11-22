package session

import (
	"errors"
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
	manager *TraceAffinityManager
	entry   *strictBinding
	once    sync.Once
}

func (m *TraceAffinityManager) AcquireStrict(key string) (*StrictLease, error) {
	if m == nil || key == "" || len(key) > 128 {
		return nil, ErrStrictSessionUnavailable
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
	l.manager.mu.RLock()
	defer l.manager.mu.RUnlock()
	return l.entry.route, l.entry.bound
}
func (l *StrictLease) Bind(route routingref.RouteRef) (routingref.RouteRef, error) {
	if l == nil || route.ChannelUID == "" || route.Kind == "" {
		return routingref.RouteRef{}, ErrStrictSessionUnavailable
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
