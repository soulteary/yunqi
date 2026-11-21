package common

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/warmup"
)

func TestStreamingErrorBodyTimeoutAllowsBackupChannel(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			closed := make(chan struct{})
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(closed)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"message":"temporarily unavailable"}}`)
				w.(http.Flusher).Flush()
				<-r.Context().Done() // Error headers/body arrive, but the response never finishes.
			}))
			defer primary.Close()
			var backupCalls atomic.Int64
			backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				backupCalls.Add(1)
				_, _ = io.WriteString(w, "data: backup\n\n")
			}))
			defer backup.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			w, _, _ := runErrorBodyFailover(t, ctx, primary.URL, backup.URL, 30, nil)
			if backupCalls.Load() != 1 || w.Body.String() != "data: backup\n\n" {
				t.Fatalf("backup calls=%d response=%q; stalled error body prevented failover", backupCalls.Load(), w.Body.String())
			}
			if ctx.Err() != nil {
				t.Fatalf("fallback waited for the client deadline: %v", ctx.Err())
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("timed-out error response was not closed")
			}
		})
	}
}

func TestHealthyStreamOutlivesErrorBodyTimeout(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 5; i++ {
			_, _ = io.WriteString(w, "data: healthy\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer primary.Close()
	var backupCalls atomic.Int64
	backup := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { backupCalls.Add(1) }))
	defer backup.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	w, _, _ := runErrorBodyFailover(t, ctx, primary.URL, backup.URL, 30, nil)
	if backupCalls.Load() != 0 || strings.Count(w.Body.String(), "data: healthy") != 5 {
		t.Fatalf("healthy SSE was truncated or retried: backup=%d body=%q", backupCalls.Load(), w.Body.String())
	}
}

func TestErrorBodyInheritedTimeoutWithMinimalEnvironment(t *testing.T) {
	const payload = `{"error":{"type":"invalid_request_error","message":"invalid request parameter"}}`
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.(http.Flusher).Flush()
		select {
		case <-time.After(25 * time.Millisecond):
			_, _ = io.WriteString(w, payload)
		case <-r.Context().Done():
		}
	}))
	defer primary.Close()
	var backupCalls atomic.Int64
	backup := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { backupCalls.Add(1) }))
	defer backup.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// A zero channel value inherits the clamped positive fallback from EnvConfig{}.
	w, _, _ := runErrorBodyFailover(t, ctx, primary.URL, backup.URL, 0, nil)
	if backupCalls.Load() != 0 || w.Code != http.StatusBadRequest || w.Body.String() != payload {
		t.Fatalf("inherited timeout lost error body: status=%d backup=%d body=%q", w.Code, backupCalls.Load(), w.Body.String())
	}
}

func TestCancelDuringErrorBodyReadDoesNotPenalizeOrRetry(t *testing.T) {
	headersSent := make(chan struct{})
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.(http.Flusher).Flush()
		close(headersSent)
		<-r.Context().Done()
	}))
	defer primary.Close()
	var backupCalls atomic.Int64
	backup := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { backupCalls.Add(1) }))
	defer backup.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-headersSent; time.Sleep(10 * time.Millisecond); cancel() }()
	w, cm, _ := runErrorBodyFailover(t, ctx, primary.URL, backup.URL, 500, nil)
	if backupCalls.Load() != 0 || w.Body.Len() != 0 || cm.IsKeyFailed("test-primary", "Messages") {
		t.Fatalf("client cancellation penalized or retried: backup=%d body=%q failed=%t", backupCalls.Load(), w.Body.String(), cm.IsKeyFailed("test-primary", "Messages"))
	}
}

func TestCommittedStreamErrorDoesNotReplayOnBackupChannel(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "data: primary\n\n")
	}))
	defer primary.Close()
	var backupCalls atomic.Int64
	backup := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { backupCalls.Add(1) }))
	defer backup.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	w, _, _ := runErrorBodyFailover(t, ctx, primary.URL, backup.URL, 30,
		func(c fiber.Ctx, resp *http.Response, _ *config.UpstreamConfig, _ string, _ []byte) (*types.Usage, error) {
			defer resp.Body.Close()
			_, _ = io.Copy(httpbridge.Writer(c), resp.Body)
			return nil, errors.New("upstream read failed after committed output")
		})
	if backupCalls.Load() != 0 || w.Body.String() != "data: primary\n\n" {
		t.Fatalf("committed output was replayed: backup=%d body=%q", backupCalls.Load(), w.Body.String())
	}
}

func runErrorBodyFailover(t *testing.T, ctx context.Context, primaryURL, backupURL string, timeoutMs int,
	handleSuccess func(fiber.Ctx, *http.Response, *config.UpstreamConfig, string, []byte) (*types.Usage, error),
) (*httptest.ResponseRecorder, *config.ConfigManager, *scheduler.ChannelScheduler) {
	t.Helper()
	cm, sc, mm, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{
		Name: "primary", ChannelUID: "ch_primary", BaseURL: primaryURL, APIKeys: []string{"test-primary"},
		Status: "active", ServiceType: "claude", Priority: 1,
	})
	t.Cleanup(cleanup)
	if err := cm.AddUpstream(config.UpstreamConfig{
		Name: "backup", ChannelUID: "ch_backup", BaseURL: backupURL, APIKeys: []string{"test-backup"},
		Status: "active", ServiceType: "claude", Priority: 2,
	}, "back"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"test-model","stream":true}`)).WithContext(ctx))
	if handleSuccess == nil {
		handleSuccess = func(c fiber.Ctx, resp *http.Response, _ *config.UpstreamConfig, _ string, _ []byte) (*types.Usage, error) {
			defer resp.Body.Close()
			_, err := io.Copy(httpbridge.Writer(c), resp.Body)
			return nil, err
		}
	}
	HandleMultiChannelFailover(c, &config.EnvConfig{}, sc,
		scheduler.ChannelKindMessages, "Messages", "test-user", "test-model", true, "",
		func(c fiber.Ctx, selection *scheduler.SelectionResult) MultiChannelAttemptResult {
			upstream := *selection.Upstream
			// Override only this test copy to avoid a one-second production minimum per case.
			upstream.RequestTimeoutMs = timeoutMs
			handled, key, idx, failure, usage, err := TryUpstreamWithAllKeys(c,
				&config.EnvConfig{}, cm, sc,
				scheduler.ChannelKindMessages, "Messages", mm, &upstream,
				[]warmup.URLLatencyResult{{URL: upstream.BaseURL}}, []byte(`{"model":"test-model","stream":true}`), nil, true,
				func(u *config.UpstreamConfig, failed map[string]bool) (string, error) {
					for _, key := range u.APIKeys {
						if !failed[key] {
							return key, nil
						}
					}
					return "", errors.New("no available key")
				},
				func(c fiber.Ctx, u *config.UpstreamConfig, _ string) (*http.Request, error) {
					return http.NewRequestWithContext(httpbridge.Request(c).Context(), http.MethodPost, u.BaseURL, strings.NewReader("{}"))
				}, nil, nil, nil, handleSuccess, "test-model", "", selection.ChannelIndex,
				sc.GetChannelLogStore(scheduler.ChannelKindMessages))
			return MultiChannelAttemptResult{Route: selection.Route, Handled: handled, Attempted: true,
				SuccessKey: key, SuccessBaseURLIdx: idx, FailoverError: failure, Usage: usage, LastError: err}
		}, nil, nil)
	return w, cm, sc
}
