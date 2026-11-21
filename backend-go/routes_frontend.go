package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/handlers"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func (a *application) registerFrontendRoutes(r *fiber.App) {
	// 静态文件服务 (嵌入的前端)
	if a.envCfg.EnableWebUI {
		handlers.ServeFrontend(r, frontendFS, a.envCfg)
	} else {
		// 纯 API 模式
		r.Get("/", func(c fiber.Ctx) error {
			httpbridge.JSON(c, 200, fiber.Map{
				"name":    "Yunqi API Gateway",
				"mode":    "API Only",
				"version": "1.0.0",
				"endpoints": fiber.Map{
					"health": "/health",
					"proxy":  "/v1/messages",
					"config": "/admin/config/save",
				},
				"message": "Web界面已禁用，此服务器运行在纯API模式下",
			})
			return nil
		})
	}

}
