package copilot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func tokenTestResponse() *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"token":"fresh","expires_at":4102444800}`))}
}

// Observing Done lets the test release a refresh only after its waiter is ready.
type tokenWaitContext struct {
	context.Context
	ready chan struct{}
	once  sync.Once
}

func (c *tokenWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.ready) })
	return c.Context.Done()
}

func TestTokenRefreshSharesTransportTimeoutWithoutRetrying(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	manager := NewTokenManager(&http.Client{Transport: tokenConcurrencyTransport(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return nil, context.DeadlineExceeded
	})})
	first := make(chan error, 1)
	go func() { _, _, err := manager.ResolveToken(context.Background(), "same-account"); first <- err }()
	<-started
	waiterCtx := &tokenWaitContext{Context: context.Background(), ready: make(chan struct{})}
	second := make(chan error, 1)
	go func() { _, _, err := manager.ResolveToken(waiterCtx, "same-account"); second <- err }()
	select {
	case <-waiterCtx.ready:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("waiter did not reach cancellation-aware wait")
	}
	close(release)
	for range 2 {
		var err error
		select {
		case err = <-first:
		case err = <-second:
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("exchange error=%v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("transport timeout triggered %d exchanges", calls.Load())
	}
}

func TestTokenExchangeDifferentAccountsRunIndependently(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	manager := NewTokenManager(&http.Client{Transport: tokenConcurrencyTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") == "Bearer account-a" {
			close(started)
			<-release
		}
		return tokenTestResponse(), nil
	})})
	first := make(chan error, 1)
	go func() { _, _, err := manager.ResolveToken(context.Background(), "account-a"); first <- err }()
	<-started
	second := make(chan error, 1)
	go func() { _, _, err := manager.ResolveToken(context.Background(), "account-b"); second <- err }()
	select {
	case err := <-second:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(time.Second):
		t.Error("unrelated account exchange blocked")
	}
	close(release)
	if err := <-first; err != nil {
		t.Error(err)
	}
}

func TestTokenRefreshDeduplicatesConcurrentCallers(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	manager := NewTokenManager(&http.Client{Transport: tokenConcurrencyTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		<-release
		return tokenTestResponse(), nil
	})})
	var ready, done sync.WaitGroup
	ready.Add(20)
	done.Add(20)
	for range 20 {
		go func() {
			defer done.Done()
			ready.Done()
			token, _, err := manager.ResolveToken(context.Background(), "same-account")
			if err != nil || token != "fresh" {
				t.Errorf("token=%q err=%v", token, err)
			}
		}()
	}
	ready.Wait()
	close(release)
	done.Wait()
	if calls.Load() != 1 {
		t.Fatalf("exchange calls=%d want=1", calls.Load())
	}
}

func TestTokenRefreshWaiterCanCancelWithoutCancelingLeader(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	manager := NewTokenManager(&http.Client{Transport: tokenConcurrencyTransport(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return tokenTestResponse(), nil
	})})
	first := make(chan error, 1)
	go func() { _, _, err := manager.ResolveToken(context.Background(), "same-account"); first <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	second := make(chan error, 1)
	go func() { _, _, err := manager.ResolveToken(ctx, "same-account"); second <- err }()
	select {
	case err := <-second:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("waiter error=%v", err)
		}
	case <-time.After(time.Second):
		t.Error("waiter ignored cancellation")
	}
	close(release)
	if err := <-first; err != nil {
		t.Error(err)
	}
	if token, _, err := manager.ResolveToken(context.Background(), "same-account"); token != "fresh" || err != nil {
		t.Fatalf("leader cache=%q %v", token, err)
	}
}

func TestCanceledTokenRefreshLeaderDoesNotPoisonOtherCallers(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	manager := NewTokenManager(&http.Client{Transport: tokenConcurrencyTransport(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return tokenTestResponse(), nil
	})})
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, _, err := manager.ResolveToken(ctx, "same-account"); first <- err }()
	<-started
	second := make(chan error, 1)
	go func() { _, _, err := manager.ResolveToken(context.Background(), "same-account"); second <- err }()
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Errorf("leader=%v", err)
	}
	if err := <-second; err != nil {
		t.Errorf("follower=%v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("exchange calls=%d want=2", calls.Load())
	}
}

func TestCanceledContextCannotReturnCachedToken(t *testing.T) {
	manager := NewTokenManager(nil)
	manager.cache[tokenCacheKey("cached")] = cachedToken{Token: "cached", RefreshAt: time.Now().Add(time.Hour), ExpiresAt: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := manager.ResolveToken(ctx, "cached"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error=%v", err)
	}
}

type tokenConcurrencyTransport func(*http.Request) (*http.Response, error)

func (f tokenConcurrencyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCachedAccountNotBlockedByOtherTokenExchange(t *testing.T) {
	started, release, firstDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	client := &http.Client{Transport: tokenConcurrencyTransport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-release
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"token":"fresh","expires_at":4102444800}`))}, nil
	})}
	manager := NewTokenManager(client)
	manager.cache[tokenCacheKey("account-b")] = cachedToken{Token: "cached-b", RefreshAt: time.Now().Add(time.Hour), ExpiresAt: time.Now().Add(time.Hour)}
	go func() { defer close(firstDone); _, _, _ = manager.ResolveToken(context.Background(), "account-a") }()
	<-started
	secondDone := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _, _, err := manager.ResolveToken(ctx, "account-b"); secondDone <- err }()
	blocked := false
	select {
	case err := <-secondDone:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(500 * time.Millisecond):
		blocked = true
	}
	close(release)
	<-firstDone
	if blocked {
		err := <-secondDone
		t.Fatalf("cached account B blocked behind unrelated account A; returned after deadline with error=%v", err)
	}
}
