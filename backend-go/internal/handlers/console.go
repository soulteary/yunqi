package handlers

import (
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// ConsoleStatus exposes an explicit allowlist of non-secret runtime information.
// Register only behind the existing management API authentication middleware.
func ConsoleStatus(env *config.EnvConfig, manager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		cfg := manager.GetConfig()
		httpbridge.Header(c, "Cache-Control", "no-store")
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"version": getVersion(), "uptimeSeconds": time.Since(startTime).Seconds(),
			"mode":     env.Env,
			"settings": fiber.Map{"port": env.Port, "bindHost": env.BindHost, "webUI": env.EnableWebUI},
			"channels": fiber.Map{"messages": len(cfg.Upstream), "chat": len(cfg.ChatUpstream),
				"responses": len(cfg.ResponsesUpstream), "gemini": len(cfg.GeminiUpstream),
				"images": len(cfg.ImagesUpstream), "vectors": len(cfg.VectorsUpstream)},
		})
		return nil
	}
}
