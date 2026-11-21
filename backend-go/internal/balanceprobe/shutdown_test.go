package balanceprobe

import (
	"context"
	"testing"
	"time"
)

func TestStopCancelsProbeLifetime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{running: true, stopCh: make(chan struct{}), lifetime: ctx, cancelLifetime: cancel, stopTimeout: time.Second}
	observed := m.lifetimeContext()
	m.Stop()
	if observed.Err() != context.Canceled {
		t.Fatal("probe context remains active")
	}
	m.Stop()
}
