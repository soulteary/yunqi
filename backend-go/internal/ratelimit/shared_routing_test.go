package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDiscoveredAdmissionDoesNotSpendExplicitChannelLimits(t *testing.T) {
	now := time.Now()
	limiter := NewChannelLimiter(Config{RPM: 1, WindowSeconds: 120, MaxConcurrent: 1}, now)
	var releases []func()
	for i := 0; i < 4; i++ {
		release, err := limiter.AcquireDiscovered(context.Background(), time.Millisecond, now)
		if err != nil {
			t.Fatalf("Redis-owned explicit limit applied locally: %v", err)
		}
		releases = append(releases, release)
	}
	state := limiter.Status(now)
	if state.WindowSize != 0 || state.ActiveRequests != 4 {
		t.Fatalf("bad local tracking %+v", state)
	}
	for _, release := range releases {
		release()
	}
	if limiter.Status(now).ActiveRequests != 0 {
		t.Fatal("in-flight tracking leaked")
	}
	limiter.SetCooldown(now.Add(time.Minute))
	if _, err := limiter.AcquireDiscovered(context.Background(), time.Millisecond, now); !errors.Is(err, ErrInCooldown) {
		t.Fatalf("cooldown bypassed: %v", err)
	}
}
func TestDiscoveredAdmissionRetainsLearnedDimensions(t *testing.T) {
	now := time.Now()
	rpm := NewChannelLimiter(Config{MaxConcurrent: 1}, now)
	if !rpm.SetDiscoveredRPM(1) {
		t.Fatal("learned RPM unavailable")
	}
	release, err := rpm.AcquireDiscovered(context.Background(), time.Millisecond, now)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := rpm.AcquireDiscovered(context.Background(), time.Millisecond, now); !errors.Is(err, ErrWindowFull) {
		t.Fatalf("learned RPM bypassed: %v", err)
	}
	concurrent := NewChannelLimiter(Config{RPM: 1}, now)
	if !concurrent.SetDiscoveredMaxConcurrent(1) {
		t.Fatal("learned concurrency unavailable")
	}
	release, err = concurrent.AcquireDiscovered(context.Background(), time.Millisecond, now)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := concurrent.AcquireDiscovered(context.Background(), time.Millisecond, now); !errors.Is(err, ErrAcquireBusy) {
		t.Fatalf("learned concurrency bypassed: %v", err)
	}
}

func TestSharedDimensionSelectionSurvivesInheritedConfigRefresh(t *testing.T) {
	now := time.Now()
	limiter := NewChannelLimiter(Config{RPM: 1, MaxConcurrent: 1}, now)
	// Simulate a scheduler/AIMD refresh restoring channel defaults between scoped
	// configuration and acquire; request-scoped ownership must still win.
	limiter.UpdateConfig(Config{RPM: 2, MaxConcurrent: 1})
	for i := 0; i < 4; i++ {
		release, err := limiter.AcquireWithSharedLimits(context.Background(), time.Millisecond, now, true, true)
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	if limiter.Status(now).WindowSize != 0 {
		t.Fatal("background inherited config spent local RPM")
	}
	// A key with its own RPM retains that local protection independently.
	release, err := limiter.AcquireWithSharedLimits(context.Background(), time.Millisecond, now, false, true)
	if err != nil {
		t.Fatal(err)
	}
	release()
	release, err = limiter.AcquireWithSharedLimits(context.Background(), time.Millisecond, now, false, true)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, err = limiter.AcquireWithSharedLimits(context.Background(), time.Millisecond, now, false, true); !errors.Is(err, ErrWindowFull) {
		t.Fatalf("explicit key RPM bypassed: %v", err)
	}
}
