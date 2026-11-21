package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// WebAuthMiddleware Web 访问控制中间件
func WebAuthMiddleware(envCfg *config.EnvConfig, cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		path := c.Path()
		isManagementPath := strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/admin")

		// OPTIONS 预检请求直接放行（浏览器 CORS 预检不携带自定义 header，不应要求认证）
		if httpbridge.Request(c).Method == "OPTIONS" {
			return c.Next()
		}

		// 数据面与健康检查允许一个渠道前缀；代理处理器仍负责验证代理密钥。
		// 必须先精确识别端点，避免 /api 或 /admin 前缀被误当成管理请求。
		if isDataPlaneRequest(httpbridge.Request(c).Method, path) {
			return c.Next()
		}

		// 管理路由中的动态参数可能带有静态文件后缀，不能因此跳过鉴权。
		if !isManagementPath && isStaticResource(path) {
			return c.Next()
		}

		// 如果禁用了 Web UI，返回 404
		if !envCfg.EnableWebUI {
			httpbridge.JSON(c, 404, fiber.Map{
				"error":   "Web界面已禁用",
				"message": "此服务器运行在纯API模式下，请通过API端点访问服务",
			})
			httpbridge.Abort(c)
			return nil
		}

		// SPA 页面路由直接交给前端处理，但需要排除 /api* 路径
		if path == "/" || path == "/index.html" || (!strings.Contains(path, ".") && !isManagementPath) {
			return c.Next()
		}

		// 检查访问密钥（管理 API + 管理端点）
		if isManagementPath {
			providedKey := getAPIKey(c)

			// 记录认证尝试
			clientIP := httpbridge.ClientIP(c)
			timestamp := time.Now().Format(time.RFC3339)

			if !envCfg.IsValidAdminAccessKey(providedKey) {
				// 认证失败 - 记录详细日志
				reason := "密钥无效"
				if providedKey == "" {
					reason = "密钥缺失"
				}
				log.Printf("[Auth-Failed] IP: %s | Path: %s | Time: %s | Reason: %s",
					clientIP, sanitizeLogPath(path, httpbridge.Request(c).Method), timestamp, reason)
				httpbridge.JSON(c, 401, fiber.Map{
					"error":   "Unauthorized",
					"message": "Invalid or missing access key",
				})
				httpbridge.Abort(c)
				return nil
			}

			// 认证成功 - 记录日志(可选，根据日志级别)
			// 如果启用了 QuietPollingLogs，则静默轮询端点日志
			if envCfg.ShouldLog("info") && (!envCfg.QuietPollingLogs || !isPollingEndpoint(path)) {
				log.Printf("[Auth-Success] IP: %s | Path: %s | Time: %s", clientIP, sanitizeLogPath(path, httpbridge.Request(c).Method), timestamp)
			}
		}

		return c.Next()
	}
}

// isDataPlaneRequest mirrors the public route shapes in routes_proxy.go and
// registerSystemRoutes. Only one optional routePrefix segment is accepted; an
// arbitrary /v1/ substring must never exempt a management path from admin auth.
func isDataPlaneRequest(method, path string) bool {
	if isDataPlaneEndpoint(method, path) {
		return true
	}
	if !strings.HasPrefix(path, "/") {
		return false
	}
	prefix, suffix, ok := strings.Cut(path[1:], "/")
	if !ok || prefix == "" || prefix == "." || prefix == ".." {
		return false
	}
	return isDataPlaneEndpoint(method, "/"+suffix)
}

func isDataPlaneEndpoint(method, path string) bool {
	switch method {
	case http.MethodGet:
		if path == "/health" || path == "/v1/models" || path == "/v1/responses" {
			return true
		}
		if model, ok := strings.CutPrefix(path, "/v1/models/"); ok {
			return model != "" && !strings.Contains(model, "/")
		}
	case http.MethodPost:
		switch path {
		case "/v1/quote", "/v1/messages", "/v1/messages/count_tokens", "/v1/responses", "/v1/responses/compact",
			"/v1/chat/completions", "/v1/images/generations", "/v1/images/edits",
			"/v1/images/variations", "/v1/embeddings":
			return true
		}
		return strings.HasPrefix(path, "/v1/alpha/") || strings.HasPrefix(path, "/v1beta/models/")
	}
	return false
}

// isPollingEndpoint 判断是否为轮询端点（前缀匹配，兼容 query string 和尾部斜杠）
// 复用 defaultSkipPrefixes 保持与 FilteredLogger 一致
func isPollingEndpoint(path string) bool {
	// 移除 query string
	if idx := strings.Index(path, "?"); idx != -1 {
		path = path[:idx]
	}
	// 移除尾部斜杠
	path = strings.TrimSuffix(path, "/")

	// 复用 logger.go 中的 defaultSkipPrefixes
	for _, prefix := range defaultSkipPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// isStaticResource 判断是否为静态资源
func isStaticResource(path string) bool {
	staticExtensions := []string{
		"/assets/", ".css", ".js", ".ico", ".png", ".jpg",
		".gif", ".svg", ".woff", ".woff2", ".ttf", ".eot",
	}

	for _, ext := range staticExtensions {
		if strings.HasPrefix(path, ext) || strings.HasSuffix(path, ext) {
			return true
		}
	}

	return false
}

// getAPIKey 获取 API 密钥
func getAPIKey(c fiber.Ctx) string {
	// 从 header 获取
	if key := httpbridge.GetHeader(c, "x-api-key"); key != "" {
		return key
	}

	if auth := httpbridge.GetHeader(c, "Authorization"); auth != "" {
		// 移除 Bearer 前缀
		return strings.TrimPrefix(auth, "Bearer ")
	}

	// 支持 Gemini SDK 的 x-goog-api-key 头部
	if key := httpbridge.GetHeader(c, "x-goog-api-key"); key != "" {
		return key
	}

	// 浏览器原生 WebSocket API 无法设置自定义请求头，只能通过握手阶段的
	// Sec-WebSocket-Protocol 传递鉴权信息（客户端将 key 作为子协议传入）。
	// 仅作为最后一个回退来源，不影响其他认证方式的行为。
	if proto := httpbridge.GetHeader(c, "Sec-WebSocket-Protocol"); proto != "" {
		return proto
	}

	return ""
}

// ContextKeyProxyKeyMask 代理 Key 掩码在 fiber.Ctx 中的存储键。
// ProxyAuthMiddleware 验证通过后写入，下游 handler 通过 GetProxyKeyMask 取出用于成本报表按用户分组。
// Item 4（渠道推荐）将直接复用此键名和 GetProxyKeyMask helper。
const ContextKeyProxyKeyMask = "proxyKeyMask"

// GetProxyKeyMask 从 fiber.Ctx 取出本次请求的代理 Key 掩码。
// 返回空字符串表示未经过 ProxyAuthMiddleware 或密钥为空。
func GetProxyKeyMask(c fiber.Ctx) string {
	v, _ := httpbridge.Value(c, ContextKeyProxyKeyMask)
	s, _ := v.(string)
	return s
}

// ProxyAuthMiddleware 代理访问控制中间件
func ProxyAuthMiddleware(envCfg *config.EnvConfig) fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := AuthenticateProxy(c, envCfg); err != nil {
			return err
		}
		if httpbridge.IsAborted(c) {
			return nil
		}
		return c.Next()
	}
}

// AuthenticateProxy also supports handlers that perform their own authentication.
// It must not advance Fiber's router when invoked from inside a terminal handler.
func AuthenticateProxy(c fiber.Ctx, envCfg *config.EnvConfig) error {
	providedKey := getAPIKey(c)
	if !envCfg.IsValidProxyAccessKey(providedKey) {
		if envCfg.ShouldLog("warn") {
			log.Printf("[Auth-Failed] 代理访问密钥验证失败 - IP: %s", httpbridge.ClientIP(c))
		}
		httpbridge.JSON(c, 401, fiber.Map{"error": "Invalid proxy access key"})
		httpbridge.Abort(c)
		return nil
	}
	sum := sha256.Sum256([]byte(providedKey))
	principalID := "key:" + hex.EncodeToString(sum[:])
	if id := envCfg.ProxyPrincipalIDs[providedKey]; id != "" {
		principalID = "principal:" + id
	}
	httpbridge.SetValue(c, ContextKeyPrincipalID, principalID)
	if mask := envCfg.ProxyKeyMaskForRequest(providedKey); mask != "" {
		httpbridge.SetValue(c, ContextKeyProxyKeyMask, mask)
	}
	return nil
}
