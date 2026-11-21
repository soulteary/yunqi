package main

import (
	"context"
	"net"
	"net/http"
	"sync"
)

// requestDrain also tracks hijacked WebSocket handlers, which http.Server.Shutdown
// does not wait for. Writers may finalize usage until done is closed.
type requestDrain struct {
	mu       sync.Mutex
	closing  bool
	active   int
	done     chan struct{}
	cancels  map[*http.Request]context.CancelFunc
	hijacked map[net.Conn]struct{}
}

type drainConnectionKey struct{}

func (d *requestDrain) connContext(ctx context.Context, c net.Conn) context.Context {
	return context.WithValue(ctx, drainConnectionKey{}, c)
}

func newRequestDrain() *requestDrain {
	return &requestDrain{done: make(chan struct{}), cancels: make(map[*http.Request]context.CancelFunc), hijacked: make(map[net.Conn]struct{})}
}
func (d *requestDrain) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		if d.closing {
			d.mu.Unlock()
			http.Error(w, "server shutting down", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		d.active++
		d.cancels[r] = cancel
		d.mu.Unlock()
		defer func() {
			cancel()
			d.mu.Lock()
			delete(d.cancels, r)
			if conn, ok := r.Context().Value(drainConnectionKey{}).(net.Conn); ok {
				delete(d.hijacked, conn)
			}
			d.active--
			if d.closing && d.active == 0 {
				close(d.done)
			}
			d.mu.Unlock()
		}()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (d *requestDrain) begin() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closing {
		d.closing = true
		if d.active == 0 {
			close(d.done)
		}
	}
}
func (d *requestDrain) wait(ctx context.Context) error {
	select {
	case <-d.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (d *requestDrain) connState(c net.Conn, state http.ConnState) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if state == http.StateHijacked {
		if d.closing {
			_ = c.Close()
		} else {
			d.hijacked[c] = struct{}{}
		}
	} else if state == http.StateClosed {
		delete(d.hijacked, c)
	}
}
func (d *requestDrain) cancelRemaining() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, cancel := range d.cancels {
		cancel()
	}
	for c := range d.hijacked {
		_ = c.Close()
		delete(d.hijacked, c)
	}
}
