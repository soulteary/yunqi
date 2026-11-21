package autopilot

import (
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// ── 路由注册 ──

// RegisterAdvisorRoutes 注册 advisor 管理 API 到给定路由组。
// 路由前缀由调用方控制（例如 router.Group("/advisor")）。
func RegisterAdvisorRoutes(router fiber.Router, store *AdvisorDecisionStore) {
	group := router.Group("/advisor")
	{
		group.Get("/stats", handleAdvisorStats(store))
		group.Get("/decisions", handleAdvisorDecisions(store))
	}
}

// ── Handler 实现 ──

// handleAdvisorStats GET /api/autopilot/advisor/stats
// 返回 advisor 决策统计汇总。
func handleAdvisorStats(store *AdvisorDecisionStore) fiber.Handler {
	return func(c fiber.Ctx) error {
		stats := store.GetStats()
		httpbridge.JSON(c, http.StatusOK, stats)
		return nil
	}
}

// handleAdvisorDecisions GET /api/autopilot/advisor/decisions
// 查询参数：
//   - limit=N  返回最近 N 条（默认 50，最大 200）
//
// 脱敏输出：不含 promptHash、不含消息正文。
func handleAdvisorDecisions(store *AdvisorDecisionStore) fiber.Handler {
	return func(c fiber.Ctx) error {
		limit := 50
		if limitStr := httpbridge.Query(c, "limit"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		if limit > 200 {
			limit = 200
		}

		decisions := store.ListRecent(limit)
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"decisions": decisions,
			"total":     len(decisions),
		})
		return nil
	}
}
