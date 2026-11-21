package common

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type trackedErrorBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *trackedErrorBody) Close() error { b.closed.Store(true); return nil }

func TestReadUpstreamErrorBodyBoundsEncodedAndDecodedBytes(t *testing.T) {
	gzipBytes := func(value string) string {
		var out bytes.Buffer
		w := gzip.NewWriter(&out)
		_, _ = io.WriteString(w, value)
		_ = w.Close()
		return out.String()
	}
	for _, tc := range []struct {
		name, encoding, body, want string
		wantError                  bool
	}{
		{"plain", "", `{"error":"busy"}`, `{"error":"busy"}`, false},
		{"compressed", "gzip", gzipBytes(`{"error":"busy"}`), `{"error":"busy"}`, false},
		{"encoded limit", "", strings.Repeat("a", maxUpstreamErrorBodyBytes+1), strings.Repeat("a", maxUpstreamErrorBodyBytes), true},
		{"decoded limit", "gzip", gzipBytes(strings.Repeat("a", maxUpstreamErrorBodyBytes+1)), strings.Repeat("a", maxUpstreamErrorBodyBytes), true},
		{"invalid compression fallback", "gzip", "not gzip", "not gzip", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedErrorBody{Reader: strings.NewReader(tc.body)}
			resp := &http.Response{Body: body, Header: http.Header{"Content-Encoding": []string{tc.encoding}}}
			data, err := readUpstreamErrorBody(context.Background(), resp, time.Second)
			if (err != nil) != tc.wantError || string(data) != tc.want {
				t.Fatalf("length=%d error=%v; want length=%d error=%t", len(data), err, len(tc.want), tc.wantError)
			}
			if !body.closed.Load() {
				t.Fatal("body was not closed")
			}
		})
	}
}

func TestReadUpstreamErrorBodyInterruptsBlockedRead(t *testing.T) {
	for _, cancelParent := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "parent cancellation"}[cancelParent], func(t *testing.T) {
			body, writer := io.Pipe()
			defer writer.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, err := readUpstreamErrorBody(ctx, &http.Response{Body: body, Header: make(http.Header)}, 20*time.Millisecond)
				result <- err
			}()
			want := context.DeadlineExceeded
			if cancelParent {
				cancel()
				want = context.Canceled
			}
			select {
			case err := <-result:
				if !errors.Is(err, want) {
					t.Fatalf("error=%v, want %v", err, want)
				}
			case <-time.After(time.Second):
				t.Fatal("blocked body read did not stop")
			}
			if _, err := writer.Write([]byte("late body")); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("reader was not closed: %v", err)
			}
		})
	}
}
