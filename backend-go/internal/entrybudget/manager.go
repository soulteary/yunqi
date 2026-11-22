package entrybudget

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/soulteary/yunqi/internal/requestscope"
)

var ErrLeaseLost = fmt.Errorf("entry budget lease lost")
var ErrRequestTimeout = fmt.Errorf("entry budget request duration exceeded")
var ErrShutdown = fmt.Errorf("entry budget runtime stopped")

type sessionKey struct{}

type Manager struct {
	mu          sync.Mutex
	path        string
	store       *Store
	sessions    map[string]*Session
	closed      bool
	closeCtx    context.Context
	cleanupDone chan struct{}
	now         func() time.Time
}

func NewManager(path string) *Manager {
	return &Manager{path: path, sessions: make(map[string]*Session), now: time.Now, cleanupDone: make(chan struct{})}
}
func (m *Manager) Store() (*Store, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrUnavailable
	}
	if m.store == nil {
		store, err := Open(m.path)
		if err != nil {
			return nil, ErrUnavailable
		}
		m.store = store
	}
	return m.store, nil
}
func (m *Manager) Start(ctx context.Context, p Policy, id string) (*Session, error) {
	normalized, err := p.Normalize()
	if err != nil {
		return nil, err
	}
	p = normalized
	store, err := m.Store()
	if err != nil {
		return nil, err
	}
	now := m.now()
	if err := store.Start(ctx, p, id, now); err != nil {
		return nil, ErrUnavailable
	}
	s := &Session{ID: id, Policy: p, manager: m, store: store, stop: make(chan struct{})}
	s.scope = requestscope.New(s.finalize)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		s.scope.Cancel(ErrShutdown)
		return nil, ErrUnavailable
	}
	s.timerMu.Lock()
	s.timer = time.AfterFunc(time.Duration(p.MaxRequestSeconds)*time.Second, func() { s.scope.Cancel(ErrRequestTimeout) })
	s.timerMu.Unlock()
	m.sessions[id] = s
	m.mu.Unlock()
	go s.heartbeat()
	return s, nil
}
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer closeCancel()
	m.closeCtx = closeCtx
	var sessions []*Session
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	store := m.store
	m.mu.Unlock()
	// Cancellation is immediate for every actual upstream, even if SQLite is
	// locked. SQLite busy handlers can outlive context cancellation, so waiting
	// for database cleanup must share one hard deadline rather than N deadlines.
	for _, s := range sessions {
		s.scope.Signal(ErrShutdown)
	}
	done := m.cleanupDone
	go func() {
		defer close(done)
		for _, s := range sessions {
			s.scope.ParentDone()
			s.finalize()
		}
		if store != nil {
			_ = store.Close()
		}
	}()
	select {
	case <-done:
		return nil
	case <-closeCtx.Done():
		return ErrUnavailable
	}

}

type Session struct {
	ID         string
	Policy     Policy
	manager    *Manager
	store      *Store
	scope      *requestscope.Source
	next       atomic.Uint64
	finishOnce sync.Once
	stop       chan struct{}
	timerMu    sync.Mutex
	timer      *time.Timer
}

func WithSession(ctx context.Context, s *Session) context.Context {
	return requestscope.WithSource(context.WithValue(ctx, sessionKey{}, s), s.scope)
}
func FromContext(ctx context.Context) *Session { s, _ := ctx.Value(sessionKey{}).(*Session); return s }
func (s *Session) ParentDone()                 { s.scope.ParentDone() }
func (s *Session) Cause() error                { return context.Cause(s.scope.Context()) }
func (s *Session) heartbeat() {
	ticker := time.NewTicker(time.Duration(LeaseSeconds/3) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-s.scope.Context().Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err := s.store.Heartbeat(ctx, s.ID, s.manager.now())
			cancel()
			if err != nil {
				s.scope.Cancel(ErrLeaseLost)
				return
			}
		}
	}
}
func (s *Session) finalize() {
	s.finishOnce.Do(func() {
		close(s.stop)
		s.timerMu.Lock()
		timer := s.timer
		s.timerMu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		reason := ""
		switch s.Cause() {
		case ErrLeaseLost:
			reason = "budget_lease_lost"
		case ErrRequestTimeout:
			reason = "request_duration_exceeded"
		case ErrShutdown:
			reason = "runtime_stopped"
		case ErrUnavailable:
			reason = "budget_storage_unavailable"
		}
		s.manager.mu.Lock()
		parent := s.manager.closeCtx
		s.manager.mu.Unlock()
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		err := s.store.Finalize(ctx, s.ID, reason, s.manager.now())
		cancel()
		if err != nil {
			log.Printf("[Entry-Budget] request=%s settlement_pending reason=storage_unavailable", s.ID)
		}
		s.manager.mu.Lock()
		delete(s.manager.sessions, s.ID)
		s.manager.mu.Unlock()
	})
}

type Attempt struct {
	session *Session
	Quote   AttemptQuote
	once    sync.Once
	err     error
}

func (s *Session) Begin(ctx context.Context, q AttemptQuote) (*Attempt, error) {
	if s.scope.Context().Err() != nil {
		return nil, ErrClosed
	}
	q.ID = fmt.Sprintf("%s.a%06d", s.ID, s.next.Add(1))
	reserveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := s.store.Reserve(reserveCtx, s.Policy, s.ID, q, s.manager.now())
	cancel()
	if err != nil {
		if err == ErrQuota {
			return nil, ErrQuota
		}
		s.scope.Cancel(ErrUnavailable)
		return nil, ErrUnavailable
	}
	return &Attempt{session: s, Quote: q}, nil
}
func (a *Attempt) Complete(charge *int64, reason string) error {
	return a.CompleteAccounting(charge, reason, "unknown", nil)
}
func (a *Attempt) CompleteAccounting(charge *int64, reason, source string, usage json.RawMessage) error {

	if a == nil {
		return nil
	}
	a.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		a.err = a.session.store.CompleteAccounting(ctx, a.session.ID, a.Quote.ID, charge, reason, source, usage)
		cancel()
		if a.err != nil {
			a.session.scope.Cancel(ErrUnavailable)
		}
	})
	return a.err
}
