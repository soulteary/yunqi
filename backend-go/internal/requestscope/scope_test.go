package requestscope

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

func TestSourceWaitsForAllUpstreamBodiesBeforeReleasing(t *testing.T) {
	var released atomic.Int32
	s := New(func() { released.Add(1) })
	ctx := WithSource(context.Background(), s)
	_, first, err := Bind(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := Bind(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.ParentDone()
	first.Close()
	first.Close()
	if released.Load() != 0 {
		t.Fatal("lease released with active upstream")
	}
	second.Close()
	s.ParentDone()
	if released.Load() != 1 {
		t.Fatalf("release count=%d", released.Load())
	}
	if _, _, err := Bind(ctx); !errors.Is(err, ErrClosed) {
		t.Fatal("work entered completed request")
	}
}
func TestSourceLossCancelsDetachedContextAndClosesBody(t *testing.T) {
	released := make(chan struct{})
	s := New(func() { close(released) })
	parent, cancel := context.WithCancel(WithSource(context.Background(), s))
	cancel()
	ctx, binding, err := Bind(context.WithoutCancel(parent))
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	defer writer.Close()
	wrapped := WrapBody(ctx, reader, binding)
	cause := errors.New("lease lost")
	s.Cancel(cause)
	select {
	case <-ctx.Done():
		if !errors.Is(context.Cause(ctx), cause) {
			t.Fatal("lost cause")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lease loss detached")
	}
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("body kept lease alive")
	}
	_ = wrapped.Close()
}

func TestCauseSurvivesWithoutCancelAndSkipsNormalCompletion(t *testing.T) {
	normal := New(nil)
	failed := New(nil)
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := context.WithoutCancel(WithSource(WithSource(parent, normal), failed))
	if Cause(ctx) != nil {
		t.Fatal("client cancellation became resource failure")
	}
	normal.ParentDone()
	if Cause(ctx) != nil {
		t.Fatal("normal source completion became resource failure")
	}
	sentinel := errors.New("resource lost")
	failed.Signal(sentinel)
	if !errors.Is(Cause(ctx), sentinel) {
		t.Fatalf("lost resource cause %v", Cause(ctx))
	}
	failed.ParentDone()
}
