package middleware

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// 默认跳过日志的路径前缀（仅 GET 请求）
var defaultSkipPrefixes = []string{
	"/health",
	"/api/messages/channels",
	"/api/responses/channels",
	"/api/gemini/channels",
	"/api/messages/global/stats",
	"/api/responses/global/stats",
	"/api/gemini/global/stats",
}

// FilteredLogger 创建一个可过滤路径的 Logger 中间件
// 仅对 GET 请求且匹配 skipPrefixes 前缀的路径跳过日志输出
// POST/PUT/DELETE 等管理操作始终记录日志以保留审计跟踪
func FilteredLogger(envCfg *config.EnvConfig, skipPrefixes ...string) fiber.Handler {
	if len(skipPrefixes) == 0 {
		skipPrefixes = defaultSkipPrefixes
	}
	return func(c fiber.Ctx) error {
		path := httpbridge.Request(c).URL.Path
		method := httpbridge.Request(c).Method
		skip := false
		if envCfg.QuietPollingLogs {
			if method == http.MethodOptions && isResponsesChannelLogsPath(path) {
				skip = true
			}
			if method == http.MethodGet {
				for _, prefix := range skipPrefixes {
					if strings.HasPrefix(path, prefix) {
						skip = true
						break
					}
				}
			}
		}
		start := time.Now()
		err := c.Next()
		if !skip {
			log.Printf("[HTTP] %s %s | %d | %s | %s", method, sanitizeLogPath(path, method), httpbridge.Writer(c).Status(), time.Since(start), httpbridge.ClientIP(c))
		}
		return err
	}
}

func isResponsesChannelLogsPath(path string) bool {
	return strings.HasPrefix(path, "/api/responses/channels/") && strings.HasSuffix(path, "/logs")
}

// sanitizeLogPath hides legacy API-key path parameters before any request or
// authentication logging. Redact the whole remainder because decoded keys may
// themselves contain slashes. New key mutation endpoints carry keys in JSON.
func sanitizeLogPath(path, method string) string {
	parts := strings.SplitN(path, "/", 7)
	if len(parts) < 7 || parts[1] != "api" || parts[3] != "channels" || parts[5] != "keys" {
		return path
	}
	switch parts[2] {
	case "messages", "chat", "responses", "gemini", "images", "vectors":
	default:
		return path
	}
	// These exact POST routes identify JSON-body operations, not path keys.
	if method == http.MethodPost {
		switch parts[6] {
		case "remove", "move-to-top", "move-to-bottom":
			return path
		}
	}
	suffix := ""
	for _, action := range []string{"/top", "/bottom"} {
		if strings.HasSuffix(parts[6], action) {
			suffix = action
			break
		}
	}
	return strings.Join(parts[:6], "/") + "/[REDACTED]" + suffix
}
