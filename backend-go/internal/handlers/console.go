package handlers

import (
	"net/http"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/gin-gonic/gin"
)

// ConsoleStatus exposes an explicit allowlist of non-secret runtime information.
// Register only behind the existing management API authentication middleware.
func ConsoleStatus(env *config.EnvConfig, manager *config.ConfigManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := manager.GetConfig()
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{
			"version": getVersion(), "uptimeSeconds": time.Since(startTime).Seconds(),
			"mode":     env.Env,
			"settings": gin.H{"port": env.Port, "bindHost": env.BindHost, "webUI": env.EnableWebUI},
			"channels": gin.H{"messages": len(cfg.Upstream), "chat": len(cfg.ChatUpstream),
				"responses": len(cfg.ResponsesUpstream), "gemini": len(cfg.GeminiUpstream),
				"images": len(cfg.ImagesUpstream), "vectors": len(cfg.VectorsUpstream)},
		})
	}
}
