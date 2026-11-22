package main

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsolatedRedisReadinessRequiresProtocolReply(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "yqr-ready-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	socket := filepath.Join(directory, "server.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var ready atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.SetDeadline(time.Now().Add(time.Second))
			request := make([]byte, 14)
			if _, err = io.ReadFull(conn, request); err == nil && ready.Load() {
				_, _ = io.WriteString(conn, "+PONG\r\n")
			}
			conn.Close()
		}
	}()
	defer func() { listener.Close(); <-done }()
	// The old helper accepted existence alone, publishing an endpoint whose
	// first application command receives EOF before Redis becomes ready.
	if _, err := os.Stat(socket); err != nil {
		t.Fatal(err)
	}
	if err := pingIsolatedRedis(context.Background(), socket); !errors.Is(err, io.EOF) {
		t.Fatalf("expected startup EOF after socket appeared, got %v", err)
	}
	exited := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := waitIsolatedRedisReady(ctx, socket, exited); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("published an unready endpoint: %v", err)
	}
	ready.Store(true)
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := waitIsolatedRedisReady(ctx2, socket, exited); err != nil {
		t.Fatal(err)
	}
}

func TestIsolatedRedisReadinessReportsExitedChild(t *testing.T) {
	exited := make(chan struct{})
	close(exited)
	if err := waitIsolatedRedisReady(context.Background(), "/nonexistent/private-redis.sock", exited); err == nil {
		t.Fatal("exited Redis child was considered ready")
	}
}
