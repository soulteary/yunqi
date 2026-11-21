package handlers

import (
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func GetCodexAutoReviewMapping(manager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		httpbridge.Header(c, "Cache-Control", "no-store")
		httpbridge.JSON(c, http.StatusOK, manager.GetCodexAutoReviewMapping())
		return nil
	}
}

func PutCodexAutoReviewMapping(manager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request struct {
			Enabled     *bool  `json:"enabled"`
			TargetModel string `json:"targetModel"`
		}
		if err := httpbridge.BindJSON(c, &request); err != nil || request.Enabled == nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "需提供 enabled 布尔值和目标模型"})
			return nil
		}
		mapping := config.CodexAutoReviewMapping{Enabled: *request.Enabled, TargetModel: strings.TrimSpace(request.TargetModel)}
		if err := mapping.Validate(); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		if err := manager.SetCodexAutoReviewMapping(mapping); err != nil {
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": "保存自动审批映射失败"})
			return nil
		}
		httpbridge.Header(c, "Cache-Control", "no-store")
		httpbridge.JSON(c, http.StatusOK, mapping)
		return nil
	}
}
