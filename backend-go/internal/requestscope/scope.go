// Package requestscope keeps leases attached to actual upstream work, including
// work whose client cancellation was deliberately detached for usage drain.
package requestscope

import (
	"context"
	"errors"
	"io"
	"sync"
)

var ErrClosed = errors.New("request resource scope closed")

type key struct{}
type Source struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelCauseFunc
	parentDone bool
	refs       int
	finished   bool
	onDone     func()
}

func New(onDone func()) *Source {
	ctx, cancel := context.WithCancelCause(context.Background())
	return &Source{ctx: ctx, cancel: cancel, onDone: onDone}
}
func WithSource(ctx context.Context, source *Source) context.Context {
	existing, _ := ctx.Value(key{}).([]*Source)
	next := append([]*Source(nil), existing...)
	next = append(next, source)
	return context.WithValue(ctx, key{}, next)
}
func (s *Source) Context() context.Context { return s.ctx }
func (s *Source) retain() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parentDone || s.finished || s.ctx.Err() != nil {
		return false
	}
	s.refs++
	return true
}
func (s *Source) maybeFinishLocked() func() {
	if !s.finished && s.parentDone && s.refs == 0 {
		s.finished = true
		return func() {
			s.cancel(ErrClosed)
			if s.onDone != nil {
				s.onDone()
			}
		}
	}
	return nil
}
func (s *Source) release() {
	s.mu.Lock()
	s.refs--
	done := s.maybeFinishLocked()
	s.mu.Unlock()
	if done != nil {
		done()
	}
}
func (s *Source) ParentDone() {
	s.mu.Lock()
	s.parentDone = true
	done := s.maybeFinishLocked()
	s.mu.Unlock()
	if done != nil {
		done()
	}
}

// Signal cancels live work without invoking a potentially blocking completion
// callback inline. Shutdown must signal every source before waiting for cleanup.
func (s *Source) Signal(cause error) {
	if cause == nil {
		cause = ErrClosed
	}
	s.cancel(cause)
}
func (s *Source) Cancel(cause error) {
	s.Signal(cause)
	s.ParentDone()
}

type Binding struct {
	once    sync.Once
	cancel  context.CancelCauseFunc
	stops   []func() bool
	sources []*Source
}

// Bind makes lease cancellation survive context.WithoutCancel.
func Bind(ctx context.Context) (context.Context, *Binding, error) {
	sources, _ := ctx.Value(key{}).([]*Source)
	if len(sources) == 0 {
		return ctx, nil, nil
	}
	b := &Binding{}
	for _, source := range sources {
		if !source.retain() {
			for i := len(b.sources) - 1; i >= 0; i-- {
				b.sources[i].release()
			}
			return ctx, nil, ErrClosed
		}
		b.sources = append(b.sources, source)
	}
	child, cancel := context.WithCancelCause(ctx)
	b.cancel = cancel
	for _, source := range sources {
		s := source
		b.stops = append(b.stops, context.AfterFunc(s.ctx, func() { cancel(context.Cause(s.ctx)) }))
	}
	return child, b, nil
}
func (b *Binding) Close() {
	if b == nil {
		return
	}
	b.once.Do(func() {
		for _, stop := range b.stops {
			stop()
		}
		b.cancel(ErrClosed)
		for i := len(b.sources) - 1; i >= 0; i-- {
			b.sources[i].release()
		}
	})
}

type body struct {
	stopMu sync.Mutex
	io.ReadCloser
	binding *Binding
	once    sync.Once
	stop    func() bool
	err     error
}

func (b *body) Close() error {
	b.once.Do(func() {
		b.stopMu.Lock()
		stop := b.stop
		b.stopMu.Unlock()
		if stop != nil {
			stop()
		}
		b.err = b.ReadCloser.Close()
		b.binding.Close()
	})
	return b.err
}

// WrapBody releases references after accounting/inner Close has completed.
func WrapBody(ctx context.Context, inner io.ReadCloser, binding *Binding) io.ReadCloser {
	if binding == nil {
		return inner
	}
	wrapped := &body{ReadCloser: inner, binding: binding}
	wrapped.stopMu.Lock()
	wrapped.stop = context.AfterFunc(ctx, func() { _ = wrapped.Close() })
	wrapped.stopMu.Unlock()
	return wrapped
}

// Cause reports a failed resource lifetime even through context.WithoutCancel.
// Normal completion is deliberately excluded from failure classification.
func Cause(ctx context.Context) error {
	sources, _ := ctx.Value(key{}).([]*Source)
	for _, source := range sources {
		if cause := context.Cause(source.ctx); cause != nil && !errors.Is(cause, ErrClosed) {
			return cause
		}
	}
	return nil
}
