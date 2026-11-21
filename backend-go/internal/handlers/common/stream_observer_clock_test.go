package common

import (
	"testing"
	"time"
)

func TestStreamActivityDoesNotEndFirstContentClock(t *testing.T) {
	start := time.Unix(100, 0)
	observer := &StreamTimeoutObserver{startedAt: start}
	// Gemini's line reader reports every transport line, including event metadata
	// and keepalives, before its semantic preflight detects content.
	for _, elapsed := range []time.Duration{10 * time.Millisecond, 30 * time.Millisecond, 80 * time.Millisecond} {
		observer.MarkStreamActivity(start.Add(elapsed))
		if observer.HasFirstContent() || observer.FirstContentMs() != 0 {
			t.Fatal("metadata/heartbeat ended first-content clock")
		}
	}
	observer.MarkFirstContent(start.Add(125 * time.Millisecond))
	if got := observer.FirstContentMs(); got != 125 {
		t.Fatalf("TTFT=%d want=125", got)
	}
	observer.MarkStreamActivity(start.Add(150 * time.Millisecond))
	observer.MarkFirstContent(start.Add(180 * time.Millisecond))
	if got := observer.FirstContentMs(); got != 125 {
		t.Fatalf("TTFT moved after later activity: %d", got)
	}
}

func TestToolCallIsDeliverableFirstContent(t *testing.T) {
	start := time.Unix(100, 0)
	observer := &StreamTimeoutObserver{startedAt: start}
	observer.MarkStreamActivity(start.Add(10 * time.Millisecond))
	observer.MarkToolCallActivity(start.Add(90 * time.Millisecond))
	if got := observer.FirstContentMs(); got != 90 {
		t.Fatalf("tool TTFT=%d", got)
	}
}

func TestPrecontentTransportDoesNotPollutePostcontentIdle(t *testing.T) {
	start := time.Unix(100, 0)
	observer := &StreamTimeoutObserver{startedAt: start}
	observer.MarkStreamActivity(start.Add(time.Second))
	observer.MarkStreamActivity(start.Add(30 * time.Second))
	observer.MarkFirstContent(start.Add(31 * time.Second))
	observer.MarkStreamActivity(start.Add(31*time.Second + 10*time.Millisecond))
	observer.Finish(start.Add(31*time.Second + 20*time.Millisecond))
	if observer.maxStreamIdleMs != 10 {
		t.Fatalf("post-content max idle=%d want=10", observer.maxStreamIdleMs)
	}
	if observer.FirstContentMs() != 31000 {
		t.Fatalf("TTFT %d", observer.FirstContentMs())
	}
}
