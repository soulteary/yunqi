package telemetry

import (
	"context"
	"sync"
	"time"
)

// PhaseSnapshot is exclusive wall-clock attribution for the logical request.
// Concurrent/nested phases use a fixed priority, so their sum cannot exceed
// totalMs. Unobserved phases are absent; observed zero remains an explicit zero.
type PhaseSnapshot struct {
	Scope    string             `json:"scope"`
	TotalMs  float64            `json:"totalMs"`
	StagesMs map[string]float64 `json:"stagesMs"`
}

var phases = []string{"finalize", "downstream_write", "tls", "connect", "dns", "queue", "upstream_first_content", "upstream_wait", "selection"}

type Timeline struct {
	mu                   sync.Mutex
	started, last, ended time.Time
	active               map[string]int
	elapsed              map[string]time.Duration
	seen                 map[string]bool
	contentSeen          bool
}
type timingContextKey struct{}

func WithTiming(ctx context.Context) (context.Context, *Timeline) {
	now := time.Now()
	t := &Timeline{started: now, last: now, active: map[string]int{}, elapsed: map[string]time.Duration{}, seen: map[string]bool{}}
	return context.WithValue(ctx, timingContextKey{}, t), t
}
func Timing(ctx context.Context) *Timeline {
	if ctx == nil {
		return nil
	}
	t, _ := ctx.Value(timingContextKey{}).(*Timeline)
	return t
}
func Phase(ctx context.Context, name string) func() {
	if t := Timing(ctx); t != nil {
		return t.beginAt(name, time.Now())
	}
	return func() {}
}
func (t *Timeline) advance(now time.Time) {
	if now.Before(t.last) {
		now = t.last
	}
	for _, phase := range phases {
		if t.active[phase] > 0 {
			t.elapsed[phase] += now.Sub(t.last)
			break
		}
	}
	t.last = now
}
func (t *Timeline) beginAt(name string, now time.Time) func() {
	allowed := false
	for _, phase := range phases {
		if phase == name {
			allowed = true
			break
		}
	}
	if !allowed {
		return func() {}
	}
	t.mu.Lock()
	if !t.ended.IsZero() || name == "upstream_first_content" && t.contentSeen {
		t.mu.Unlock()
		return func() {}
	}
	t.advance(now)
	t.active[name]++
	t.seen[name] = true
	t.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { t.endAt(name, time.Now()) }) }
}
func (t *Timeline) endAt(name string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.ended.IsZero() {
		return
	}
	t.advance(now)
	if t.active[name] > 0 {
		t.active[name]--
	}
}
func MarkFirstContent(ctx context.Context) {
	if t := Timing(ctx); t != nil {
		t.mu.Lock()
		defer t.mu.Unlock()
		if !t.ended.IsZero() {
			return
		}
		t.advance(time.Now())
		t.contentSeen = true
		t.active["upstream_first_content"] = 0
	}
}
func (t *Timeline) Finish() { t.finishAt(time.Now()) }
func (t *Timeline) finishAt(now time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ended.IsZero() {
		t.advance(now)
		t.ended = t.last
	}
}
func (t *Timeline) Snapshot() *PhaseSnapshot {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	end := t.ended
	if end.IsZero() {
		t.advance(time.Now())
		end = t.last
	}
	s := &PhaseSnapshot{Scope: "request_elapsed_exclusive", TotalMs: float64(end.Sub(t.started)) / float64(time.Millisecond), StagesMs: make(map[string]float64, len(t.seen))}
	for phase := range t.seen {
		s.StagesMs[phase] = float64(t.elapsed[phase]) / float64(time.Millisecond)
	}
	return s
}
