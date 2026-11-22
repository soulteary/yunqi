// Package entrylease provides optional fail-closed Redis admission leases.
package entrylease

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/soulteary/yunqi/internal/redisconn"
	"github.com/soulteary/yunqi/internal/requestscope"
)

var ErrUnavailable = errors.New("shared proxy admission unavailable")
var ErrLeaseLost = errors.New("shared proxy admission lease lost")
var ErrConcurrency = errors.New("shared proxy concurrency exceeded")
var ErrRPM = errors.New("shared proxy request rate exceeded")
var namespacePattern = regexp.MustCompile(`^[a-zA-Z0-9:_-]{1,64}$`)

type Manager struct {
	client      *redis.Client
	namespace   string
	ttl         time.Duration
	mu          sync.Mutex
	closed      bool
	leases      map[string]*Lease
	lastWarning atomic.Int64
}

func New(redisURL, namespace string, ttl time.Duration) (*Manager, error) {

	if namespace == "" {
		namespace = "yunqi"
	}
	if !namespacePattern.MatchString(namespace) || ttl < 5*time.Second || ttl > 5*time.Minute {
		return nil, ErrUnavailable
	}
	client, err := redisconn.New(redisURL)
	if err != nil {
		return nil, ErrUnavailable
	}
	return newManager(client, namespace, ttl), nil

}

func newManager(client *redis.Client, namespace string, ttl time.Duration) *Manager {
	return &Manager{client: client, namespace: namespace, ttl: ttl, leases: make(map[string]*Lease)}
}
func (m *Manager) warn() {
	now := time.Now().Unix()
	last := m.lastWarning.Load()
	if now-last >= 30 && m.lastWarning.CompareAndSwap(last, now) {
		log.Print("[Proxy-Admission] shared lease unavailable; admission remains fail closed")
	}
}
func (m *Manager) keys(principal string) []string {
	sum := sha256.Sum256([]byte(principal))
	base := m.namespace + ":admission:{" + hex.EncodeToString(sum[:]) + "}:"
	return []string{base + "leases", base + "rpm"}
}

// Acquire uses a server-generated nonce, never a caller's request-id header.
func (m *Manager) Acquire(ctx context.Context, principal string, concurrent, rpm int) (*Lease, time.Duration, error) {
	if principal == "" || len(principal) > 256 || concurrent < 0 || rpm < 0 {
		return nil, 0, ErrUnavailable
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, 0, ErrUnavailable
	}
	id := hex.EncodeToString(nonce[:])
	keys := m.keys(principal)
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return nil, 0, ErrUnavailable
	}
	started := time.Now()
	call, cancel := context.WithTimeout(ctx, 2*time.Second)
	result, err := acquireScript.Run(call, m.client, keys, id, m.ttl.Milliseconds(), concurrent, rpm).Int64Slice()
	cancel()
	if err != nil || len(result) != 2 {
		m.warn()
		return nil, 0, ErrUnavailable
	}
	switch result[0] {
	case 0:
		return nil, time.Duration(result[1]) * time.Millisecond, ErrConcurrency
	case 2:
		return nil, time.Duration(result[1]) * time.Millisecond, ErrRPM
	case 1:
	default:
		m.warn()
		return nil, 0, ErrUnavailable
	}
	lease := &Lease{id: id, keys: keys, manager: m, stop: make(chan struct{}), done: make(chan struct{})}
	lease.source = requestscope.New(lease.release)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		lease.source.Cancel(ErrLeaseLost)
		return nil, 0, ErrUnavailable
	}
	m.leases[id] = lease
	m.mu.Unlock()
	if !lease.refreshDeadline(started.Add(m.ttl)) {
		return nil, 0, ErrUnavailable
	}
	go lease.renew()
	return lease, 0, nil
}

type leaseKey struct{}
type Lease struct {
	id              string
	keys            []string
	manager         *Manager
	source          *requestscope.Source
	once            sync.Once
	stop            chan struct{}
	done            chan struct{}
	watchMu         sync.Mutex
	watchGeneration uint64
	watchdog        *time.Timer
}

func WithLease(ctx context.Context, l *Lease) context.Context {
	return requestscope.WithSource(context.WithValue(ctx, leaseKey{}, l), l.source)
}
func FromContext(ctx context.Context) *Lease {
	lease, _ := ctx.Value(leaseKey{}).(*Lease)
	return lease
}
func (l *Lease) ParentDone()  { l.source.ParentDone() }
func (l *Lease) Cause() error { return context.Cause(l.source.Context()) }
func (l *Lease) renew() {
	ticker := time.NewTicker(l.manager.ttl / 3)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-l.source.Context().Done():
			return
		case <-ticker.C:
			started := time.Now()
			call, cancel := context.WithTimeout(context.Background(), min(2*time.Second, l.manager.ttl/3))
			result, err := renewScript.Run(call, l.manager.client, l.keys[:1], l.id, l.manager.ttl.Milliseconds()).Int64()
			cancel()
			if err != nil || result != 1 {
				l.manager.warn()
				l.source.Cancel(ErrLeaseLost)
				return
			}
			if !l.refreshDeadline(started.Add(l.manager.ttl)) {
				return
			}
		}
	}
}
func (l *Lease) release() {
	l.once.Do(func() {
		l.watchMu.Lock()
		l.watchGeneration++
		if l.watchdog != nil {
			l.watchdog.Stop()
		}
		l.watchMu.Unlock()
		close(l.stop)
		call, cancel := context.WithTimeout(context.Background(), min(time.Second, l.manager.ttl/3))
		_, err := releaseScript.Run(call, l.manager.client, l.keys[:1], l.id).Result()
		cancel()
		if err != nil {
			l.manager.warn()
		}
		l.manager.mu.Lock()
		delete(l.manager.leases, l.id)
		l.manager.mu.Unlock()
		close(l.done)
	})
}
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	leases := make([]*Lease, 0, len(m.leases))
	for _, lease := range m.leases {
		leases = append(leases, lease)
	}
	m.mu.Unlock()
	// All actual upstreams are cancelled concurrently before a bounded wait. A
	// lost Redis connection leaves at most ttl until server-side lease recovery.
	for _, lease := range leases {
		lease.source.Signal(ErrLeaseLost)
	}
	for _, lease := range leases {
		go lease.source.ParentDone()
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for _, lease := range leases {
		select {
		case <-lease.done:
		case <-deadline.C:
			_ = m.client.Close()
			return ErrUnavailable
		}
	}
	return m.client.Close()
}

// Use the RPC start, not reply time: server execution cannot precede that
// instant, making this local monotonic deadline conservative under slow replies.
// A late renewal acknowledgement can never resurrect already cancelled work.
func (l *Lease) refreshDeadline(deadline time.Time) bool {
	l.watchMu.Lock()
	l.watchGeneration++
	generation := l.watchGeneration
	if l.watchdog != nil {
		l.watchdog.Stop()
	}
	if l.source.Context().Err() != nil {
		l.watchMu.Unlock()
		return false
	}
	duration := time.Until(deadline)
	if duration <= 0 {
		l.source.Signal(ErrLeaseLost)
		l.watchMu.Unlock()
		l.source.ParentDone()
		return false
	}
	l.watchdog = time.AfterFunc(duration, func() {
		l.watchMu.Lock()
		expired := l.watchGeneration == generation && l.source.Context().Err() == nil
		if expired {
			l.source.Signal(ErrLeaseLost)
		}
		l.watchMu.Unlock()
		if expired {
			l.manager.warn()
			l.source.ParentDone()
		}
	})
	l.watchMu.Unlock()
	return true
}

func (m *Manager) AcquireContext(ctx context.Context, principal string, concurrent, rpm int) (context.Context, func(), time.Duration, error) {
	lease, retry, err := m.Acquire(ctx, principal, concurrent, rpm)
	if err != nil {
		return ctx, nil, retry, err
	}
	return WithLease(ctx, lease), lease.ParentDone, 0, nil
}
