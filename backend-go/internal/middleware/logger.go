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
			log.Printf("[HTTP] %s %s | %d | %s | %s", method, path, httpbridge.Writer(c).Status(), time.Since(start), httpbridge.ClientIP(c))
		}
		return err
	}
}

func isResponsesChannelLogsPath(path string) bool {
	return strings.HasPrefix(path, "/api/responses/channels/") && strings.HasSuffix(path, "/logs")
}
