// Package handlers 提供 HTTP 处理器
package handlers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
)

// GetCircuitBreaker 获取熔断器运行时配置
// getCurrent: 返回当前运行时生效的熔断器参数的函数
func GetCircuitBreaker(getCurrent func() metrics.CircuitBreakerParams, envCfg *config.EnvConfig) fiber.Handler {
	return func(c fiber.Ctx) error {
		params := getCurrent()
		httpbridge.JSON(c, 200, fiber.Map{
			"windowSize":                   params.WindowSize,
			"failureThreshold":             params.FailureThreshold,
			"consecutiveFailuresThreshold": params.ConsecutiveFailuresThreshold,
			"requestTimeoutMs":             config.GetRuntimeRequestTimeoutMs(envCfg.RequestTimeout),
			"responseHeaderTimeoutMs":      config.GetRuntimeResponseHeaderTimeoutMs(envCfg.ResponseHeaderTimeout * 1000),
			"streamFirstContentTimeoutMs":  params.StreamFirstContentTimeoutMs,
			"streamInactivityTimeoutMs":    params.StreamInactivityTimeoutMs,
			"streamToolCallIdleTimeoutMs":  params.StreamToolCallIdleTimeoutMs,
		})
		return nil
	}
}

// SetCircuitBreaker 更新熔断器运行时配置
func SetCircuitBreaker(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req struct {
			WindowSize                   *int     `json:"windowSize"`
			FailureThreshold             *float64 `json:"failureThreshold"`
			ConsecutiveFailuresThreshold *int     `json:"consecutiveFailuresThreshold"`
			RequestTimeoutMs             *int     `json:"requestTimeoutMs"`
			ResponseHeaderTimeoutMs      *int     `json:"responseHeaderTimeoutMs"`
			StreamFirstContentTimeoutMs  *int     `json:"streamFirstContentTimeoutMs"`
			StreamInactivityTimeoutMs    *int     `json:"streamInactivityTimeoutMs"`
			StreamToolCallIdleTimeoutMs  *int     `json:"streamToolCallIdleTimeoutMs"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "请求格式无效"})
			return nil
		}

		// 参数校验
		if req.WindowSize != nil {
			if *req.WindowSize < 3 || *req.WindowSize > 100 {
				httpbridge.JSON(c, 400, fiber.Map{"error": "windowSize 必须在 3-100 之间"})
				return nil
			}
		}
		if req.FailureThreshold != nil {
			if *req.FailureThreshold < 0.01 || *req.FailureThreshold > 1.0 {
				httpbridge.JSON(c, 400, fiber.Map{"error": "failureThreshold 必须在 0.01-1.0 之间"})
				return nil
			}
		}
		if req.ConsecutiveFailuresThreshold != nil {
			if *req.ConsecutiveFailuresThreshold < 1 || *req.ConsecutiveFailuresThreshold > 100 {
				httpbridge.JSON(c, 400, fiber.Map{"error": "consecutiveFailuresThreshold 必须在 1-100 之间"})
				return nil
			}
		}
		if req.RequestTimeoutMs != nil {
			if err := config.ValidateRuntimeRequestTimeoutMs(*req.RequestTimeoutMs); err != nil {
				httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
				return nil
			}
		}
		if req.ResponseHeaderTimeoutMs != nil {
			if err := config.ValidateRuntimeResponseHeaderTimeoutMs(*req.ResponseHeaderTimeoutMs); err != nil {
				httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
				return nil
			}
		}
		if req.StreamFirstContentTimeoutMs != nil {
			if *req.StreamFirstContentTimeoutMs < 5000 || *req.StreamFirstContentTimeoutMs > 300000 {
				httpbridge.JSON(c, 400, fiber.Map{"error": "streamFirstContentTimeoutMs 必须在 5000-300000 之间"})
				return nil
			}
		}
		if req.StreamInactivityTimeoutMs != nil {
			if *req.StreamInactivityTimeoutMs < 1000 || *req.StreamInactivityTimeoutMs > 180000 {
				httpbridge.JSON(c, 400, fiber.Map{"error": "streamInactivityTimeoutMs 必须在 1000-180000 之间"})
				return nil
			}
		}
		if req.StreamToolCallIdleTimeoutMs != nil {
			if *req.StreamToolCallIdleTimeoutMs < 30000 || *req.StreamToolCallIdleTimeoutMs > 300000 {
				httpbridge.JSON(c, 400, fiber.Map{"error": "streamToolCallIdleTimeoutMs 必须在 30000-300000 之间"})
				return nil
			}
		}

		if err := cfgManager.SetCircuitBreakerConfig(config.CircuitBreakerConfig{
			WindowSize:                   req.WindowSize,
			FailureThreshold:             req.FailureThreshold,
			ConsecutiveFailuresThreshold: req.ConsecutiveFailuresThreshold,
			RequestTimeoutMs:             req.RequestTimeoutMs,
			ResponseHeaderTimeoutMs:      req.ResponseHeaderTimeoutMs,
			StreamFirstContentTimeoutMs:  req.StreamFirstContentTimeoutMs,
			StreamInactivityTimeoutMs:    req.StreamInactivityTimeoutMs,
			StreamToolCallIdleTimeoutMs:  req.StreamToolCallIdleTimeoutMs,
		}); err != nil {
			httpbridge.JSON(c, 500, fiber.Map{"error": "保存配置失败"})
			return nil
		}

		// 返回更新后的完整配置
		updated := cfgManager.GetCircuitBreakerConfig()
		httpbridge.JSON(c, 200, fiber.Map{
			"success": true,
			"circuitBreaker": fiber.Map{
				"windowSize":                   updated.WindowSize,
				"failureThreshold":             updated.FailureThreshold,
				"consecutiveFailuresThreshold": updated.ConsecutiveFailuresThreshold,
				"requestTimeoutMs":             updated.RequestTimeoutMs,
				"responseHeaderTimeoutMs":      updated.ResponseHeaderTimeoutMs,
				"streamFirstContentTimeoutMs":  updated.StreamFirstContentTimeoutMs,
				"streamInactivityTimeoutMs":    updated.StreamInactivityTimeoutMs,
				"streamToolCallIdleTimeoutMs":  updated.StreamToolCallIdleTimeoutMs,
			},
		})
		return nil
	}
}
