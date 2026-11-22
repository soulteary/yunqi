package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// The TCP listener remains owned throughout the test and forwards exclusively
// to a new private Unix socket. No free-port race can connect to an existing
// user's Redis. Every child/connection is closed via its own handle.
func newIsolatedRedisEndpoint(t *testing.T) (string, func()) {
	t.Helper()
	binary := os.Getenv("YUNQI_TEST_REDIS_SERVER")
	if binary == "" {
		var err error
		binary, err = exec.LookPath("redis-server")
		if err != nil {
			t.Skip("set YUNQI_TEST_REDIS_SERVER for isolated Redis integration tests")
		}
	}
	directory, err := os.MkdirTemp("/tmp", "yqr-api-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	socket := filepath.Join(directory, "server.sock")
	output, err := os.Create(filepath.Join(directory, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "--port", "0", "--unixsocket", socket, "--unixsocketperm", "700", "--save", "", "--appendonly", "no", "--daemonize", "no", "--dir", directory)
	command.Stdout = output
	command.Stderr = output
	if err := command.Start(); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = command.Wait(); close(exited) }()
	stop := sync.OnceFunc(func() {
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
			_ = command.Process.Kill()
			<-exited
		}
		_ = output.Close()
	})
	t.Cleanup(stop)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err = waitIsolatedRedisReady(ctx, socket, exited)
	cancel()
	if err != nil {
		log, _ := os.ReadFile(filepath.Join(directory, "server.log"))
		t.Fatalf("private Redis not ready: %v\n%s", err, log)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var active sync.Map
	var workers sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			incoming, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			active.Store(incoming, struct{}{})
			go func() {
				defer workers.Done()
				defer active.Delete(incoming)
				backend, err := net.DialTimeout("unix", socket, time.Second)
				if err != nil {
					_ = incoming.Close()
					return
				}
				closeBoth := sync.OnceFunc(func() { _ = incoming.Close(); _ = backend.Close() })
				defer closeBoth()
				sent := make(chan struct{})
				go func() { _, _ = io.Copy(backend, incoming); closeBoth(); close(sent) }()
				_, _ = io.Copy(incoming, backend)
				closeBoth()
				<-sent
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-acceptDone
		active.Range(func(key, value interface{}) bool { _ = key.(net.Conn).Close(); return true })
		workers.Wait()
	})
	return "redis://" + listener.Addr().String(), stop
}

// A Unix socket pathname can exist before listen/Redis initialization completes.
// Probe only the private child; never retry application transactions to hide it.
func waitIsolatedRedisReady(ctx context.Context, socket string, exited <-chan struct{}) error {
	var lastErr error
	for {
		select {
		case <-exited:
			return fmt.Errorf("Redis child exited before readiness: %v", lastErr)
		case <-ctx.Done():
			return fmt.Errorf("Redis readiness: %w (last probe: %v)", ctx.Err(), lastErr)
		default:
		}
		lastErr = pingIsolatedRedis(ctx, socket)
		if lastErr == nil {
			return nil
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
		case <-exited:
			timer.Stop()
		}
	}
}

func pingIsolatedRedis(ctx context.Context, socket string) error {
	probe, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(probe, "unix", socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := probe.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	if _, err := io.WriteString(conn, "*1\r\n$4\r\nPING\r\n"); err != nil {
		return err
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return err
	}
	if reply != "+PONG\r\n" {
		return fmt.Errorf("unexpected Redis readiness response %q", reply)
	}
	return nil
}
