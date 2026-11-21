package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestDrainWaitsForFinalization(t *testing.T) {
	d := newRequestDrain()
	entered := make(chan struct{})
	release := make(chan struct{})
	completed := make(chan struct{})
	handler := d.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; close(completed) }))
	go handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", nil))
	<-entered
	d.begin()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d.wait(ctx) == nil {
		t.Fatal("prematurely drained")
	}
	close(release)
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.wait(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-completed:
	default:
		t.Fatal("finalization skipped")
	}
	d.begin()
}
func TestRequestDrainCancelsHijackedAndInFlight(t *testing.T) {
	d := newRequestDrain()
	server, client := net.Pipe()
	defer client.Close()
	entered := make(chan struct{})
	finished := make(chan struct{})
	req := httptest.NewRequest("GET", "/v1/responses", nil)
	req = req.WithContext(d.connContext(req.Context(), server))
	go func() {
		d.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.connState(server, http.StateHijacked)
			close(entered)
			<-r.Context().Done()
		})).ServeHTTP(httptest.NewRecorder(), req)
		close(finished)
	}()
	<-entered
	d.begin()
	d.cancelRemaining()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.wait(ctx); err != nil {
		t.Fatal(err)
	}
	<-finished
	d.mu.Lock()
	n := len(d.hijacked)
	d.mu.Unlock()
	if n != 0 {
		t.Fatal("hijacked connection retained")
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("hijacked connection remained open")
	}
}
