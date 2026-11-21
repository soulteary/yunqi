package handlers

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
)

// GetModelStatsHistory 获取按模型分组的历史统计
func GetModelStatsHistory(metricsManager *metrics.MetricsManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		durationStr := httpbridge.Query(c, "duration", "24h")

		var duration time.Duration
		var err error

		duration, err = parseExtendedDuration(durationStr)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid duration parameter. Use: 1h, 6h, 24h, or today"})
			return nil
		}

		// 模型统计基于内存 requestHistory，只保留 24 小时数据
		maxDuration := 24 * time.Hour
		if duration > maxDuration {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Model stats only support up to 24 hours. Use: 1h, 6h, 24h, or today"})
			return nil
		}

		// 根据 duration 自动选择聚合粒度
		interval := selectIntervalForDuration(httpbridge.Query(c, "interval"), duration)

		models := metricsManager.GetModelStatsHistory(duration, interval)
		httpbridge.JSON(c, 200, fiber.Map{
			"models":   models,
			"duration": durationStr,
			"interval": interval.String(),
		})
		return nil
	}
}
