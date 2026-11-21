package main

import (
	"github.com/soulteary/yunqi/internal/handlers"
	"github.com/gin-gonic/gin"
)

func (a *application) registerFrontendRoutes(r *gin.Engine) {
	// 静态文件服务 (嵌入的前端)
	if a.envCfg.EnableWebUI {
		handlers.ServeFrontend(r, frontendFS, a.envCfg)
	} else {
		// 纯 API 模式
		r.GET("/", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"name":    "Yunqi API Gateway",
				"mode":    "API Only",
				"version": "1.0.0",
				"endpoints": gin.H{
					"health": "/health",
					"proxy":  "/v1/messages",
					"config": "/admin/config/save",
				},
				"message": "Web界面已禁用，此服务器运行在纯API模式下",
			})
		})
	}

}
