package middleware

import (
	"net/http"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/telemetry"
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
		limit := env.ProxyMaxConcurrency
		if limit <= 0 {
			return c.Next()
		}
		id := GetPrincipalID(c)
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
		return c.Next()
	}
}
