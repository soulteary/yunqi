package telemetry

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func fixedTimeline() *Timeline {
	start := time.Unix(100, 0)
	return &Timeline{started: start, last: start, active: map[string]int{}, elapsed: map[string]time.Duration{}, seen: map[string]bool{}}
}
func TestPhaseTimingExclusiveOverlapAndZero(t *testing.T) {
	t1 := fixedTimeline()
	start := t1.started
	_ = t1.beginAt("queue", start)
	_ = t1.beginAt("dns", start.Add(2*time.Millisecond))
	t1.endAt("dns", start.Add(5*time.Millisecond))
	_ = t1.beginAt("connect", start.Add(5*time.Millisecond))
	t1.endAt("connect", start.Add(8*time.Millisecond))
	t1.endAt("queue", start.Add(8*time.Millisecond))
	_ = t1.beginAt("tls", start.Add(8*time.Millisecond))
	t1.endAt("tls", start.Add(8*time.Millisecond))
	t1.finishAt(start.Add(10 * time.Millisecond))
	s := t1.Snapshot()
	if s.TotalMs != 10 || s.StagesMs["queue"] != 2 || s.StagesMs["dns"] != 3 || s.StagesMs["connect"] != 3 {
		t.Fatalf("%+v", s)
	}
	if value, ok := s.StagesMs["tls"]; !ok || value != 0 {
		t.Fatal("observed zero was omitted")
	}
	if _, ok := s.StagesMs["selection"]; ok {
		t.Fatal("unknown invented")
	}
	sum := 0.0
	for _, v := range s.StagesMs {
		sum += v
	}
	if sum > s.TotalMs || math.IsNaN(sum) {
		t.Fatal(s)
	}
	if t1.Snapshot().TotalMs != s.TotalMs {
		t.Fatal("finished timing keeps growing")
	}
}
func TestPhaseTimingConcurrentAndBounded(t *testing.T) {
	ctx, tl := WithTiming(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				end := Phase(ctx, "downstream_write")
				end()
				end()
				_ = tl.Snapshot()
			}
		}()
	}
	wg.Wait()
	tl.Finish()
	s := tl.Snapshot()
	if len(s.StagesMs) != 1 || s.StagesMs["downstream_write"] > s.TotalMs {
		t.Fatal(s)
	}
}
func TestNetworkTimingRealTransportAndReuse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	client := server.Client()
	for i := 0; i < 2; i++ {
		ctx, tl := WithTiming(context.Background())
		req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
		req, end := NetworkTiming(req)
		resp, err := client.Do(req)
		end()
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		tl.Finish()
		s := tl.Snapshot()
		if _, ok := s.StagesMs["upstream_wait"]; !ok {
			t.Fatal("missing response wait", s)
		}
		if _, ok := s.StagesMs["tls"]; ok {
			t.Fatal("invented TLS on plain HTTP")
		}
		if i == 1 {
			if _, ok := s.StagesMs["connect"]; ok {
				t.Fatal("invented connect on reused connection")
			}
		}
	}
}
