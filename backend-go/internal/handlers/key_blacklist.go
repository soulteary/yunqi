package handlers

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// RestoreBlacklistedKey 恢复被拉黑的 API Key
// POST /api/{type}/channels/:id/keys/restore
// Body: {"apiKey": "sk-xxx"}
func RestoreBlacklistedKey(cfgManager *config.ConfigManager, apiType string) fiber.Handler {
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

		if err := cfgManager.RestoreKey(apiType, id, req.APIKey); err != nil {
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

// RestoreKeyModel 手动移除 (Key, 模型) 组合级限制
// POST /api/{type}/channels/:id/keys/restore-model
// Body: {"apiKey": "sk-xxx", "model": "gpt-5.6-sol"}
func RestoreKeyModel(cfgManager *config.ConfigManager, apiType string) fiber.Handler {
	return func(c fiber.Ctx) error {
		idStr := httpbridge.Param(c, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid channel ID"})
			return nil
		}

		var req struct {
			APIKey string `json:"apiKey"`
			Model  string `json:"model"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil || req.APIKey == "" || req.Model == "" {
			httpbridge.JSON(c, 400, fiber.Map{"error": "apiKey and model are required"})
			return nil
		}

		if err := cfgManager.RestoreKeyModel(apiType, id, req.APIKey, req.Model); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{
			"message": "(Key, 模型) 限制已移除",
			"success": true,
		})
		return nil
	}
}

// DisableGroupModel 人工禁用目标 Key 所属配额组的模型。
func DisableGroupModel(cfgManager *config.ConfigManager, apiType string) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := strconv.Atoi(httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid channel ID"})
			return nil
		}
		var req struct {
			APIKey string `json:"apiKey"`
			Model  string `json:"model"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil || strings.TrimSpace(req.APIKey) == "" || strings.TrimSpace(req.Model) == "" {
			httpbridge.JSON(c, 400, fiber.Map{"error": "apiKey and model are required"})
			return nil
		}
		quotaGroup, affectedKeyCount, err := cfgManager.DisableGroupModel(apiType, id, req.APIKey, req.Model)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{
			"success":          true,
			"quotaGroup":       quotaGroup,
			"model":            strings.TrimSpace(req.Model),
			"affectedKeyCount": affectedKeyCount,
		})
		return nil
	}
}

// RestoreGroupModel 移除人工分组模型限制。
func RestoreGroupModel(cfgManager *config.ConfigManager, apiType string) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := strconv.Atoi(httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid channel ID"})
			return nil
		}
		var req struct {
			APIKey     string `json:"apiKey"`
			QuotaGroup string `json:"quotaGroup"`
			Model      string `json:"model"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil || strings.TrimSpace(req.Model) == "" ||
			(strings.TrimSpace(req.APIKey) == "" && strings.TrimSpace(req.QuotaGroup) == "") {
			httpbridge.JSON(c, 400, fiber.Map{"error": "model and apiKey or quotaGroup are required"})
			return nil
		}
		quotaGroup, affectedKeyCount, err := cfgManager.RestoreGroupModel(apiType, id, req.APIKey, req.QuotaGroup, req.Model)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{
			"success":          true,
			"quotaGroup":       quotaGroup,
			"model":            strings.TrimSpace(req.Model),
			"affectedKeyCount": affectedKeyCount,
		})
		return nil
	}
}
