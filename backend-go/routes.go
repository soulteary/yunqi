package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/telemetry"
)

// newRouter only registers middleware and routes. All runtime dependencies and
// workers are wired by runService before the server starts accepting requests.
func (a *application) newRouter() *fiber.App {
	// 创建路由器（使用自定义 Logger，根据 QUIET_POLLING_LOGS 配置过滤轮询日志）
	r := httpbridge.NewApp()
	r.Use(middleware.FilteredLogger(a.envCfg))
	r.Use(recover.New())
	if a.monitoring == nil {
		a.monitoring = telemetry.NewRegistry()
	}
	r.Use(a.monitoring.Middleware())

	// 配置 CORS
	r.Use(middleware.CORSMiddleware(a.envCfg))

	// 静态资源 Gzip 压缩（排除 API 端点）
	r.Use(middleware.GzipMiddleware())

	// Web UI 访问控制中间件
	r.Use(middleware.WebAuthMiddleware(a.envCfg, a.cfgManager))

	r.Use(middleware.ProxyAdmissionMiddleware(a.envCfg))

	a.registerSystemRoutes(r)
	apiGroup := r.Group("/api")
	a.registerManagementRoutes(apiGroup)
	a.registerProxyRoutes(r)
	a.registerFrontendRoutes(r)
	return r
}
