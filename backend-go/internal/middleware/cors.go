package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// CORSMiddleware only grants cross-origin access when explicitly enabled.
func CORSMiddleware(envCfg *config.EnvConfig) fiber.Handler {
	return func(c fiber.Ctx) error {
		if envCfg.EnableCORS {
			// Both accepted and rejected origins affect the response headers. Include
			// requests without Origin so shared caches cannot reuse an incorrect variant.
			httpbridge.Writer(c).Header().Add("Vary", "Origin")
			if origin := httpbridge.GetHeader(c, "Origin"); origin != "" {
				setCORSHeaders(c, envCfg, origin)
			}
		}

		// Preflight requests do not carry authentication. Without allow headers the
		// browser still rejects cross-origin access when disabled or not permitted.
		if httpbridge.Request(c).Method == http.MethodOptions {
			httpbridge.AbortWithStatus(c, http.StatusNoContent)
			return nil
		}
		return c.Next()
	}
}

func setCORSHeaders(c fiber.Ctx, envCfg *config.EnvConfig, origin string) {
	allowOrigin := strings.TrimSpace(envCfg.CORSOrigin)
	if envCfg.IsDevelopment() && isLocalhostOrigin(origin) {
		allowOrigin = origin
	}
	if allowOrigin != "*" && allowOrigin != origin {
		return
	}
	httpbridge.Header(c, "Access-Control-Allow-Origin", allowOrigin)
	httpbridge.Header(c, "Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	httpbridge.Header(c, "Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, x-goog-api-key")
	if allowOrigin != "*" {
		httpbridge.Header(c, "Access-Control-Allow-Credentials", "true")
	}
}

func isLocalhostOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		strings.EqualFold(parsed.Hostname(), "localhost") && parsed.User == nil &&
		parsed.Path == "" && parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == ""
}
