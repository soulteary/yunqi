package telemetry

import (
	"context"
	"fmt"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"io"
	"sort"
	"strings"
)

type requestMetricContext struct {
	registry *Registry
	endpoint string
}
type metricContextKey struct{}

func Event(ctx context.Context, event, outcome string) {
	if ctx == nil {
		return
	}
	binding, ok := ctx.Value(metricContextKey{}).(requestMetricContext)
	if !ok {
		return
	}
	switch event {
	case "failover", "limiter", "sse_terminal":
	default:
		return
	}
	allowed := map[string]bool{"http_error": true, "transport_error": true, "invalid_response": true, "concurrency": true, "channel": true, "scope": true, "completed": true, "failed": true, "incomplete": true, "error": true, "stalled": true, "repetition": true, "canceled": true, "unknown": true}
	if !allowed[outcome] {
		outcome = "other"
	}
	traceEvent(ctx, event, outcome)
	r := binding.registry
	r.mu.Lock()
	if r.events == nil {
		r.events = make(map[string]uint64)
	}
	r.events[binding.endpoint+"|"+event+"|"+outcome]++
	r.mu.Unlock()
}
func writeEvents(w io.Writer, events map[string]uint64) {
	names := []struct{ event, name, help string }{{"failover", "yunqi_failover_decisions_total", "Retry or failover decisions after upstream failures."}, {"limiter", "yunqi_limiter_rejections_total", "Local admission or rate limiter rejections."}, {"sse_terminal", "yunqi_sse_terminals_total", "Final stream handler outcomes, including canceled or unknown."}}
	keys := make([]string, 0, len(events))
	for key := range events {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, entry := range names {
		_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", entry.name, entry.help, entry.name)
		for _, key := range keys {
			parts := strings.Split(key, "|")
			if parts[1] == entry.event {
				_, _ = fmt.Fprintf(w, "%s{endpoint=%q,outcome=%q} %d\n", entry.name, parts[0], parts[2], events[key])
			}
		}
	}
}

// BridgeMiddleware instruments each WebSocket response.create HTTP bridge using
// the service registry inherited in its trusted parent context. The outer
// responses_ws handler measures only the connection lifetime.
func BridgeMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		binding, ok := httpbridge.Request(c).Context().Value(metricContextKey{}).(requestMetricContext)
		if !ok {
			return c.Next()
		}
		return binding.registry.Middleware()(c)
	}
}
