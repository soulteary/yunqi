package common

import (
	"context"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/telemetry"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLifecycleTraceAndTLSPhaseTimingCompose(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{}`) }))
	defer server.Close()
	ctx, tl := telemetry.WithTiming(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	connected, firstByte := 0, 0
	resp, err := SendRequestWithLifecycleTrace(req, &config.UpstreamConfig{BaseURL: server.URL, InsecureSkipVerify: true}, &config.EnvConfig{RequestTimeout: 5000, ResponseHeaderTimeout: 5}, false, "Chat", &RequestLifecycleTrace{OnConnected: func() { connected++ }, OnFirstResponseByte: func() { firstByte++ }})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	tl.Finish()
	s := tl.Snapshot()
	if connected != 1 || firstByte != 1 {
		t.Fatalf("existing callbacks: connected=%d firstbyte=%d", connected, firstByte)
	}
	for _, phase := range []string{"connect", "tls", "upstream_wait"} {
		if _, ok := s.StagesMs[phase]; !ok {
			t.Fatalf("missing %s: %+v", phase, s)
		}
	}
	sum := 0.0
	for _, v := range s.StagesMs {
		sum += v
	}
	if sum > s.TotalMs+1e-6 {
		t.Fatal(s)
	}
}
