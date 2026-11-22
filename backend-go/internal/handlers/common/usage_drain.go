package common

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/converters"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/tidwall/gjson"
)

const usageDrainByteLimit = 1 << 20
const usageDrainOutputTokenLimit = 1024

type noUsageDrainKey struct{}

// WithoutUsageDrain keeps explicit WebSocket cancel/interrupt immediate.
func WithoutUsageDrain(ctx context.Context) context.Context {
	return context.WithValue(ctx, noUsageDrainKey{}, true)
}

type usageDrainLifetime struct {
	client      context.Context
	upstream    context.Context
	cancel      context.CancelFunc
	timeout     time.Duration
	mu          sync.Mutex
	closed      bool
	timer       *time.Timer
	stopClient  func() bool
	stopService func() bool
}

func prepareUsageDrainRequest(req *http.Request, env *config.EnvConfig, isStream, disabled bool) (*http.Request, *usageDrainLifetime) {
	if req == nil || env == nil || !isStream || disabled || env.UsageDrainTimeoutMs <= 0 || env.ServiceContext == nil || req.Context().Err() != nil || req.Context().Value(noUsageDrainKey{}) == true || req.GetBody == nil {
		return req, nil
	}
	body, err := req.GetBody()
	if err != nil {
		return req, nil
	}
	raw, err := io.ReadAll(io.LimitReader(body, 2<<20))
	_ = body.Close()
	if err != nil || !json.Valid(raw) {
		return req, nil
	}
	found := false
	for _, path := range []string{"max_output_tokens", "max_tokens", "max_completion_tokens", "generationConfig.maxOutputTokens"} {
		v := gjson.GetBytes(raw, path)
		if !v.Exists() {
			continue
		}
		n := v.Float()
		if v.Type != gjson.Number || n <= 0 || n > usageDrainOutputTokenLimit || n != float64(int64(n)) {
			return req, nil
		}
		found = true
	}
	if !found {
		return req, nil
	}
	timeout := time.Duration(env.UsageDrainTimeoutMs) * time.Millisecond
	if timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	// Preserve requestscope/budget values. Deadlines and service cancellation remain
	// hard limits; only an early client cancellation receives the drain allowance.
	ctx, cancel := context.WithCancel(context.WithoutCancel(req.Context()))
	if deadline, ok := req.Context().Deadline(); ok {
		var deadlineCancel context.CancelFunc
		ctx, deadlineCancel = context.WithDeadline(ctx, deadline)
		baseCancel := cancel
		cancel = func() { deadlineCancel(); baseCancel() }
	}
	lifetime := &usageDrainLifetime{client: req.Context(), upstream: ctx, cancel: cancel, timeout: timeout}
	lifetime.stopClient = context.AfterFunc(req.Context(), func() {
		lifetime.mu.Lock()
		defer lifetime.mu.Unlock()
		if lifetime.closed {
			return
		}
		if req.Context().Err() == context.DeadlineExceeded {
			lifetime.cancel()
			return
		}
		lifetime.timer = time.AfterFunc(timeout, lifetime.cancel)
	})
	lifetime.stopService = context.AfterFunc(env.ServiceContext, cancel)
	return req.WithContext(ctx), lifetime
}
func (s *usageDrainLifetime) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
	s.stopClient()
	s.stopService()
	s.cancel()
}

type usageDrainChunk struct {
	body []byte
	err  error
}

// Only pump owns upstream reads. Consumer cancellation never starts a second
// reader, and underlying Close (including budget settlement) happens after drain.
type usageDrainBody struct {
	capture   *usageCapture
	lifetime  *usageDrainLifetime
	chunks    chan usageDrainChunk
	done      chan struct{}
	remainder []byte
	closeOnce sync.Once
	pumpErr   error
}

func newUsageDrainBody(capture *usageCapture, lifetime *usageDrainLifetime) *usageDrainBody {
	body := &usageDrainBody{capture: capture, lifetime: lifetime, chunks: make(chan usageDrainChunk, 1), done: make(chan struct{})}
	go body.pump()
	return body
}
func (b *usageDrainBody) closeUpstream() { b.closeOnce.Do(func() { _ = b.capture.Close() }) }
func (b *usageDrainBody) pump() {
	defer close(b.done)
	defer b.lifetime.close()
	defer b.closeUpstream()
	stop := context.AfterFunc(b.lifetime.upstream, b.closeUpstream)
	defer stop()
	drained := 0
	for {
		size := 32 << 10
		if b.lifetime.client.Err() != nil && usageDrainByteLimit-drained < size {
			size = usageDrainByteLimit - drained
		}
		if size <= 0 {
			return
		}
		buffer := make([]byte, size)
		n, err := b.capture.Read(buffer)
		if b.lifetime.client.Err() != nil {
			drained += n
			if drained >= usageDrainByteLimit {
				return
			}
		} else {
			chunk := usageDrainChunk{body: buffer[:n], err: err}
			select {
			case b.chunks <- chunk:
			case <-b.lifetime.client.Done():
				drained += n
			case <-b.lifetime.upstream.Done():
				return
			}
		}
		if err != nil {
			b.pumpErr = err
			return
		}
		if b.lifetime.upstream.Err() != nil {
			return
		}
	}
}
func (b *usageDrainBody) Read(p []byte) (int, error) {
	if err := b.lifetime.client.Err(); err != nil {
		return 0, err
	}
	if len(b.remainder) > 0 {
		n := copy(p, b.remainder)
		b.remainder = b.remainder[n:]
		return n, nil
	}
	select {
	case <-b.lifetime.client.Done():
		return 0, b.lifetime.client.Err()
	case chunk := <-b.chunks:
		if err := b.lifetime.client.Err(); err != nil {
			return 0, err
		}
		n := copy(p, chunk.body)
		b.remainder = chunk.body[n:]
		if len(b.remainder) > 0 {
			return n, nil
		}
		return n, chunk.err
	case <-b.done:
		select {
		case chunk := <-b.chunks:
			n := copy(p, chunk.body)
			b.remainder = chunk.body[n:]
			return n, chunk.err
		default:
			if b.pumpErr != nil {
				return 0, b.pumpErr
			}
			return 0, io.EOF
		}
	}
}
func (b *usageDrainBody) Close() error {
	if b.lifetime.client.Err() == nil {
		b.lifetime.close()
		b.closeUpstream()
	}
	return nil
}
func (b *usageDrainBody) finalUsage(protocol string) *types.Usage {
	<-b.done
	// Flush a final SSE data line even when the upstream omitted its newline.
	b.capture.annotate(&types.Usage{}, protocol)
	b.capture.mu.Lock()
	raw, _ := json.Marshal(b.capture.raw)
	b.capture.mu.Unlock()
	var object map[string]interface{}
	_ = json.Unmarshal(raw, &object)
	parsed := converters.ExtractUsageMetrics(object)
	usage := &types.Usage{InputTokens: parsed.InputTokens, OutputTokens: parsed.OutputTokens, CacheCreationInputTokens: parsed.CacheCreationInputTokens, CacheReadInputTokens: parsed.CacheReadInputTokens, CacheCreation5mInputTokens: parsed.CacheCreation5mInputTokens, CacheCreation1hInputTokens: parsed.CacheCreation1hInputTokens, CacheTTL: parsed.CacheTTL}
	if parsed.InputTokensDetails != nil {
		usage.CacheReadInputTokens = parsed.InputTokensDetails.CachedTokens
	}
	switch protocol {
	case "openai", "chat", "responses", "copilot":
		usage.PromptTokensTotal = parsed.InputTokens
	}
	b.capture.annotate(usage, protocol)
	if !errors.Is(b.pumpErr, io.EOF) {
		usage.Provenance.UsageSource = "unknown"
		usage.Provenance.Reason = "client_cancel_usage_drain_incomplete"
	}
	return usage
}

type usageDrainWriter struct {
	httpbridge.ResponseWriter
	client context.Context
}

func (w *usageDrainWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *usageDrainWriter) Write(body []byte) (int, error) {
	if err := w.client.Err(); err != nil {
		return 0, err
	}
	return w.ResponseWriter.Write(body)
}
func (w *usageDrainWriter) WriteString(body string) (int, error) { return w.Write([]byte(body)) }
func (w *usageDrainWriter) WriteHeader(status int) {
	if w.client.Err() == nil {
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *usageDrainWriter) WriteHeaderNow() {
	if w.client.Err() == nil {
		w.ResponseWriter.WriteHeaderNow()
	}
}
func (w *usageDrainWriter) Flush() {
	if w.client.Err() == nil {
		w.ResponseWriter.Flush()
	}
}
