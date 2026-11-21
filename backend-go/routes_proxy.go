package main

import (
	"github.com/soulteary/yunqi/internal/autopilot"
	"github.com/soulteary/yunqi/internal/handlers/alpha"
	"github.com/soulteary/yunqi/internal/handlers/chat"
	"github.com/soulteary/yunqi/internal/handlers/gemini"
	"github.com/soulteary/yunqi/internal/handlers/images"
	"github.com/soulteary/yunqi/internal/handlers/messages"
	"github.com/soulteary/yunqi/internal/handlers/responses"
	"github.com/soulteary/yunqi/internal/handlers/vectors"
	"github.com/gin-gonic/gin"
)

func (a *application) registerProxyRoutes(r *gin.Engine) {
	// 代理端点 - Messages API
	messagesHandler := messages.Handler(a.envCfg, a.cfgManager, a.channelScheduler)
	r.POST("/v1/messages", messagesHandler)
	r.POST("/:routePrefix/v1/messages", messagesHandler)

	countTokensHandler := messages.CountTokensHandler(a.envCfg, a.cfgManager, a.channelScheduler)
	r.POST("/v1/messages/count_tokens", countTokensHandler)
	r.POST("/:routePrefix/v1/messages/count_tokens", countTokensHandler)

	// 代理端点 - Models API（转发到上游）
	modelsHandler := messages.ModelsHandler(a.envCfg, a.cfgManager, a.channelScheduler)
	r.GET("/v1/models", modelsHandler)
	r.GET("/:routePrefix/v1/models", modelsHandler)

	modelsDetailHandler := messages.ModelsDetailHandler(a.envCfg, a.cfgManager, a.channelScheduler)
	r.GET("/v1/models/:model", modelsDetailHandler)
	r.GET("/:routePrefix/v1/models/:model", modelsDetailHandler)

	// 代理端点 - Responses API
	responsesHandler := responses.Handler(a.envCfg, a.cfgManager, a.sessionManager, a.channelScheduler)
	r.POST("/v1/responses", responsesHandler)
	r.POST("/:routePrefix/v1/responses", responsesHandler)

	// Responses WebSocket: 支持 Codex 原生 response.create over WebSocket。
	responsesWebSocketHandler := responses.WebSocketHandler(a.envCfg, a.cfgManager, a.sessionManager, a.channelScheduler)
	r.GET("/v1/responses", responsesWebSocketHandler)
	r.GET("/:routePrefix/v1/responses", responsesWebSocketHandler)

	compactHandler := responses.CompactHandler(a.envCfg, a.cfgManager, a.sessionManager, a.channelScheduler)
	// Phase 4 Item 7: 注入本地任务模板到 gin.Context（供 compact 层查询模板，nil 时使用默认提示词）
	compactWithTemplates := func(c *gin.Context) {
		if a.autopilotManager != nil && a.autopilotManager.TaskTemplateStore() != nil {
			autopilot.SetTaskTemplateStore(c, a.autopilotManager.TaskTemplateStore())
		}
		compactHandler(c)
	}
	r.POST("/v1/responses/compact", compactWithTemplates)
	r.POST("/:routePrefix/v1/responses/compact", compactWithTemplates)

	// 代理端点 - Codex 记忆层数据面（history/notes 透传，粘 Responses 渠道池）
	// 路径格式：/v1/alpha/history/v2/* 或 /v1/alpha/notes/v2/*
	alphaHandler := alpha.Handler(a.envCfg, a.cfgManager, a.channelScheduler)
	r.POST("/v1/alpha/*rest", alphaHandler)
	r.POST("/:routePrefix/v1/alpha/*rest", alphaHandler)

	// 代理端点 - Gemini API (原生协议)
	// 使用通配符捕获 model:action 格式，如 gemini-pro:generateContent
	// 路径格式：/v1beta/models/{model}:generateContent (Gemini 原生格式)
	geminiHandler := gemini.Handler(a.envCfg, a.cfgManager, a.channelScheduler)
	r.POST("/v1beta/models/*modelAction", geminiHandler)
	r.POST("/:routePrefix/v1beta/models/*modelAction", geminiHandler)

	// 代理端点 - Chat Completions API (OpenAI 兼容)
	chatHandler := chat.Handler(a.envCfg, a.cfgManager, a.channelScheduler)
	r.POST("/v1/chat/completions", chatHandler)
	r.POST("/:routePrefix/v1/chat/completions", chatHandler)

	// 代理端点 - Images API (OpenAI Images 兼容)
	imagesHandler := images.Handler(a.envCfg, a.cfgManager, a.channelScheduler)
	r.POST("/v1/images/generations", imagesHandler)
	r.POST("/:routePrefix/v1/images/generations", imagesHandler)
	r.POST("/v1/images/edits", imagesHandler)
	r.POST("/:routePrefix/v1/images/edits", imagesHandler)
	r.POST("/v1/images/variations", imagesHandler)
	r.POST("/:routePrefix/v1/images/variations", imagesHandler)

	// 代理端点 - Embeddings API (OpenAI Embeddings 兼容)
	vectorsHandler := vectors.Handler(a.envCfg, a.cfgManager, a.channelScheduler)
	r.POST("/v1/embeddings", vectorsHandler)
	r.POST("/:routePrefix/v1/embeddings", vectorsHandler)

}
