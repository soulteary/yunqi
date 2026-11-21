package main

import (
	"net/http"

	"github.com/soulteary/yunqi/internal/handlers"
	channelsv2 "github.com/soulteary/yunqi/internal/handlers/channels"
	"github.com/soulteary/yunqi/internal/handlers/copilot"
	"github.com/soulteary/yunqi/internal/handlers/logicalchannels"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/presetstore"
	"github.com/gin-gonic/gin"
)

func (a *application) registerSystemRoutes(r *gin.Engine) {
	// 健康检查端点（固定路径 /health，与 Dockerfile HEALTHCHECK 保持一致）
	healthHandler := handlers.HealthCheck(a.envCfg, a.cfgManager)
	r.GET("/health", healthHandler)
	r.GET("/:routePrefix/health", healthHandler)

	// 配置保存端点
	r.POST("/admin/config/save", handlers.SaveConfigHandler(a.cfgManager))

	// 开发信息端点
	if a.envCfg.IsDevelopment() {
		r.GET("/admin/dev/info", handlers.DevInfo(a.envCfg, a.cfgManager))
	}
}

func (a *application) registerManagementRoutes(apiGroup *gin.RouterGroup) {
	discoveryModelFetchers := buildChannelDiscoveryModelFetchers(a.cfgManager)
	apiGroup.GET("/console/status", handlers.ConsoleStatus(a.envCfg, a.cfgManager))
	apiGroup.POST("/copilot/oauth/device/code", copilot.RequestDeviceCode())
	apiGroup.POST("/copilot/oauth/token", copilot.PollAccessToken())
	apiGroup.POST("/copilot/oauth/verify", copilot.VerifyToken())

	// 预置数据（订阅来源分类等），前端表单选项来源；独立于 autopilot 开关。
	apiGroup.GET("/presets", presetstore.Handler(presetstore.Default()))
	apiGroup.GET("/presets/status", presetstore.StatusHandler(a.presetUpdater))

	// 配额真相快照：全部渠道的来源分级/headroom/窗口重置时间/饱和状态。
	// autopilot 关闭时 quotaManager 为 nil，返回空列表（fail-open）。
	apiGroup.GET("/quota/channels", handlers.GetQuotaChannels(a.quotaManager))

	apiGroup.POST("/channel-discovery", handlers.ChannelDiscoveryWithModelFetchers(a.cfgManager, discoveryModelFetchers))
	// 快速探活：仅探一个真实模型以定 primaryKind，不做全量协议/能力探测。
	apiGroup.POST("/channel-discovery-fast", handlers.ChannelDiscoveryFast(a.cfgManager))

	a.registerChannelRoutes(apiGroup)
	logicalchannels.RegisterRoutes(apiGroup, a.cfgManager, a.channelScheduler)
	channelsv2.RegisterRoutes(apiGroup, a.cfgManager, a.channelScheduler)
	a.registerAutopilotRoutes(apiGroup)
	apiGroup.GET("/racing/config", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"enabled": a.cfgManager.GetRacingEnabled()})
	})
	apiGroup.PUT("/racing/config", func(c *gin.Context) {
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求体，需提供 enabled 布尔值"})
			return
		}
		if err := a.cfgManager.SetRacingEnabled(*req.Enabled); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存竞速配置失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"enabled": *req.Enabled})
	})

	// 熔断器运行时设置
	apiGroup.GET("/settings/circuit-breaker", handlers.GetCircuitBreaker(a.messagesMetricsManager.GetCircuitBreakerConfig, a.envCfg))
	apiGroup.PUT("/settings/circuit-breaker", handlers.SetCircuitBreaker(a.cfgManager))

	// 会话调度看板 API
	convDeps := &handlers.ConversationHandlerDeps{
		Tracker:          a.conversationTracker,
		OverrideManager:  a.overrideManager,
		ChannelScheduler: a.channelScheduler,
		ConfigManager:    a.cfgManager,
	}
	apiGroup.GET("/conversations", handlers.GetConversations(convDeps))
	apiGroup.POST("/conversations/:id/override", handlers.SetConversationOverride(convDeps))
	apiGroup.DELETE("/conversations/:id/override", handlers.RemoveConversationOverride(convDeps))
	apiGroup.GET("/conversations/settings", handlers.GetConversationSettings(convDeps))
	apiGroup.PUT("/conversations/settings", handlers.UpdateConversationSettings(convDeps))

	// Phase 4 Item 2: 成本报表 API（按 user/model/key 分组聚合）
	apiGroup.GET("/reports/cost", handlers.GetCostReport(&handlers.CostReportDeps{
		MetricsManagers: map[string]*metrics.MetricsManager{
			"messages":  a.messagesMetricsManager,
			"responses": a.responsesMetricsManager,
			"chat":      a.chatMetricsManager,
			"gemini":    a.geminiMetricsManager,
			"images":    a.imagesMetricsManager,
			"vectors":   a.vectorsMetricsManager,
		},
	}))
	// Phase 4 Item 5: 批量渠道管理 API（导入/导出/模板）
	apiGroup.POST("/channels/export", handlers.ExportChannels(a.envCfg, a.cfgManager))
	apiGroup.GET("/channels/export", handlers.ExportAllChannels(a.envCfg, a.cfgManager))
	apiGroup.POST("/channels/import", handlers.ImportChannels(a.cfgManager))
	apiGroup.POST("/channels/import/confirm", handlers.ImportChannelsConfirm(a.cfgManager))
	apiGroup.GET("/channels/templates", handlers.GetChannelTemplates())
	apiGroup.GET("/channels/provider-templates", handlers.GetProviderTemplates())
}
