package middleware

import (
	"net/http"
	"strings"
	"sync"

	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/entrylease"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/telemetry"
	"strconv"
	"time"
)

// ContextKeyPrincipalID is a full cryptographic identity, never a display mask.
const ContextKeyPrincipalID = "proxyPrincipalID"

func GetPrincipalID(c fiber.Ctx) string {
	value, _ := httpbridge.Value(c, ContextKeyPrincipalID)
	id, _ := value.(string)
	return id
}

type principalGate struct {
	mu     sync.Mutex
	active map[string]int
}

// Each immutable environment belongs to one service instance. The HTTP router
// and its WebSocket bridge must share counters rather than create separate pools.
var principalGates sync.Map

// ProxyAdmissionMiddleware holds an entry-key lease for the complete synchronous
// HTTP/SSE/WebSocket handler lifetime. HTTP and WebSocket requests share counters for the same environment.
func ProxyAdmissionMiddleware(env *config.EnvConfig) fiber.Handler {
	value, _ := principalGates.LoadOrStore(env, &principalGate{active: make(map[string]int)})
	gate := value.(*principalGate)
	return func(c fiber.Ctx) error {
		path := c.Path()
		if !isDataPlaneRequest(httpbridge.Request(c).Method, path) || (httpbridge.Request(c).Method == http.MethodGet && strings.HasSuffix(path, "/v1/responses")) || path == "/health" || strings.HasSuffix(path, "/health") {
			return c.Next()
		}
		if err := AuthenticateProxy(c, env); err != nil {
			return err
		}
		if httpbridge.IsAborted(c) {
			return nil
		}
		// Read-only self accounting stays available while a principal is busy.
		if httpbridge.Request(c).Method == http.MethodGet && strings.HasSuffix(path, "/v1/budget") {
			return c.Next()
		}
		limit := env.ProxyMaxConcurrency
		id := GetPrincipalID(c)
		if limit > 0 {
			gate.mu.Lock()
			if gate.active[id] >= limit {
				gate.mu.Unlock()
				telemetry.Event(httpbridge.Request(c).Context(), "limiter", "concurrency")
				httpbridge.Writer(c).Header().Set("Retry-After", "1")
				httpbridge.JSON(c, http.StatusTooManyRequests, fiber.Map{"error": "proxy_concurrency_exceeded"})
				httpbridge.Abort(c)
				return nil
			}
			gate.active[id]++
			gate.mu.Unlock()
			defer func() {
				gate.mu.Lock()
				gate.active[id]--
				if gate.active[id] == 0 {
					delete(gate.active, id)
				}
				gate.mu.Unlock()
			}()
		}
		if env.ProxyRedisURL != "" && (limit > 0 || env.ProxyRedisMaxRPM > 0) {
			if env.ProxyRedisAdmission == nil {
				return rejectSharedAdmission(c, entrylease.ErrUnavailable, 0)
			}
			leaseContext, release, retry, err := env.ProxyRedisAdmission.AcquireContext(httpbridge.Request(c).Context(), id, limit, env.ProxyRedisMaxRPM)
			if err != nil {
				return rejectSharedAdmission(c, err, retry)
			}
			httpbridge.SetRequest(c, httpbridge.Request(c).WithContext(leaseContext))
			defer release()
		}

		return c.Next()
	}
}

func rejectSharedAdmission(c fiber.Ctx, err error, retry time.Duration) error {
	status, code := 503, "proxy_shared_admission_unavailable"
	if errors.Is(err, entrylease.ErrConcurrency) {
		status, code = 429, "proxy_concurrency_exceeded"
	}
	if errors.Is(err, entrylease.ErrRPM) {
		status, code = 429, "proxy_rpm_exceeded"
	}
	telemetry.Event(httpbridge.Request(c).Context(), "limiter", "rejected")
	if status == 429 {
		seconds := max(1, int((retry+time.Second-1)/time.Second))
		httpbridge.Writer(c).Header().Set("Retry-After", strconv.Itoa(seconds))
	}
	httpbridge.JSON(c, status, fiber.Map{"error": code})
	httpbridge.Abort(c)
	return nil
}
