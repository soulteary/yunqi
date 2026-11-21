package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// CORSMiddleware CORS 中间件
func CORSMiddleware(envCfg *config.EnvConfig) fiber.Handler {
	return func(c fiber.Ctx) error {
		origin := httpbridge.GetHeader(c, "Origin")
		if origin != "" {
			setCORSHeaders(c, envCfg, origin)
		}

		// OPTIONS 预检请求不携带自定义认证头，始终直接返回 CORS 响应。
		if httpbridge.Request(c).Method == "OPTIONS" {
			httpbridge.AbortWithStatus(c, 204)
			return nil
		}

		// 如果未启用 CORS，非跨域请求保持原有行为。
		if !envCfg.EnableCORS && origin == "" {
			return c.Next()
		}

		return c.Next()
	}
}

func setCORSHeaders(c fiber.Ctx, envCfg *config.EnvConfig, origin string) {
	allowOrigin := envCfg.CORSOrigin
	if envCfg.IsDevelopment() && strings.Contains(origin, "localhost") {
		allowOrigin = origin
	}
	if !envCfg.EnableCORS {
		allowOrigin = origin
	}
	httpbridge.Header(c, "Access-Control-Allow-Origin", allowOrigin)
	httpbridge.Header(c, "Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	httpbridge.Header(c, "Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, x-goog-api-key")
	if allowOrigin != "*" {
		httpbridge.Header(c, "Access-Control-Allow-Credentials", "true")
	}
}
