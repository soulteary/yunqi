package handlers

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// SuspendAPIKey 手动暂停指定 API Key（设置 Enabled=false）
// POST /api/{type}/channels/:id/keys/suspend
// Body: {"apiKey": "sk-xxx"}
func SuspendAPIKey(cfgManager *config.ConfigManager, apiType string) fiber.Handler {
	return func(c fiber.Ctx) error {
		idStr := httpbridge.Param(c, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid channel ID"})
			return nil
		}

		var req struct {
			APIKey string `json:"apiKey"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil || req.APIKey == "" {
			httpbridge.JSON(c, 400, fiber.Map{"error": "apiKey is required"})
			return nil
		}

		if err := cfgManager.SuspendKey(apiType, id, req.APIKey); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{
			"message": "Key 已暂停",
			"success": true,
		})
		return nil
	}
}

// ResumeAPIKey 手动恢复指定 API Key（移除 Enabled 限制）
// POST /api/{type}/channels/:id/keys/resume
// Body: {"apiKey": "sk-xxx"}
func ResumeAPIKey(cfgManager *config.ConfigManager, apiType string) fiber.Handler {
	return func(c fiber.Ctx) error {
		idStr := httpbridge.Param(c, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid channel ID"})
			return nil
		}

		var req struct {
			APIKey string `json:"apiKey"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil || req.APIKey == "" {
			httpbridge.JSON(c, 400, fiber.Map{"error": "apiKey is required"})
			return nil
		}

		if err := cfgManager.ResumeKey(apiType, id, req.APIKey); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{
			"message": "Key 已恢复",
			"success": true,
		})
		return nil
	}
}
