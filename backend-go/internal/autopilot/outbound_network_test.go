package autopilot

import (
	"net/http"
	"testing"
	"time"
)

func TestProbeWorkerPreservesTimeoutAndSkipsUnavailableEgress(t *testing.T) {
	worker := NewProbeWorker(nil, ProbeWorkerConfig{ProbeTimeout: 2 * time.Second, QuietLogs: true})
	var got time.Duration
	worker.SetClientResolver(func(uid string, timeout time.Duration) (*http.Client, bool) {
		if uid != "ch-bound" {
			t.Fatalf("wrong channel %s", uid)
		}
		got = timeout
		return nil, false
	})
	result := worker.executeProbe(&KeyEndpointProfile{ChannelUID: "ch-bound", ChannelKind: "chat", ServiceType: "openai", BaseURL: "http://127.0.0.1:1", EndpointUID: "ep-bound"}, "test-only")
	if got != 2*time.Second || !result.LocalEgressError || result.Success {
		t.Fatalf("timeout=%s result=%+v", got, result)
	}
}
