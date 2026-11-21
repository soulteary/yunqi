package autopilot

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// handleProviderQualityBudget GET /api/health-center/provider-quality/budget
// 返回当前进程的手动 L3 探测预算，不触发任何上游请求。
func handleProviderQualityBudget(probe *ProviderQualityProbe) fiber.Handler {
	return func(c fiber.Ctx) error {
		if probe == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{
				"error": "ProviderQuality 探测器不可用",
				"code":  "probe_unavailable",
			})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, probe.BudgetState())
		return nil
	}
}

// handleProviderQualityProbe POST /api/health-center/provider-quality/probe
// 执行固定 canary；请求和响应均不包含明文 API Key 或模型原始输出。
func handleProviderQualityProbe(probe *ProviderQualityProbe) fiber.Handler {
	return func(c fiber.Ctx) error {
		if probe == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{
				"error": "ProviderQuality 探测器不可用",
				"code":  "probe_unavailable",
			})
			return nil
		}
		var req ProviderQualityProbeRequest
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{
				"error": "请求体格式无效",
				"code":  "invalid_json",
			})
			return nil
		}

		result, err := probe.Probe(httpbridge.Request(c).Context(), req)
		if err == nil {
			httpbridge.JSON(c, http.StatusOK, result)
			return nil
		}

		var probeErr *ProviderQualityProbeError
		if errors.As(err, &probeErr) {
			response := fiber.Map{
				"error": probeErr.Message,
				"code":  probeErr.Code,
			}
			if probe != nil && probeErr.Code == "probe_budget_exhausted" {
				response["budget"] = probe.BudgetState()
			}
			httpbridge.JSON(c, probeErr.HTTPStatus, response)
			return nil
		}
		httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{
			"error": "ProviderQuality 探测失败",
			"code":  "probe_failed",
		})
		return nil
	}
}
