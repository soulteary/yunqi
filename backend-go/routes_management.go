package main

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/handlers"
	channelsv2 "github.com/soulteary/yunqi/internal/handlers/channels"
	"github.com/soulteary/yunqi/internal/handlers/copilot"
	"github.com/soulteary/yunqi/internal/handlers/logicalchannels"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/presetstore"
)

func (a *application) registerSystemRoutes(r *fiber.App) {
	r.Get("/access", handlers.AccessPortal(a.envCfg))

	// 健康检查端点（固定路径 /health，与 Dockerfile HEALTHCHECK 保持一致）
	healthHandler := handlers.HealthCheck(a.envCfg, a.cfgManager)
	r.Get("/health", healthHandler)
	r.Get("/:routePrefix/health", healthHandler)

	// 配置保存端点
	r.Post("/admin/config/save", handlers.SaveConfigHandler(a.cfgManager))

	// 开发信息端点
	if a.envCfg.IsDevelopment() {
		r.Get("/admin/dev/info", handlers.DevInfo(a.envCfg, a.cfgManager))
	}
}

func (a *application) registerManagementRoutes(apiGroup fiber.Router) {
	handlers.RegisterEntryKeyRoutes(apiGroup, a.cfgManager)
	handlers.RegisterPrincipalBudgetRoutes(apiGroup, a.envCfg, a.cfgManager)
	handlers.RegisterAdminAccessRoutes(apiGroup, a.envCfg, a.cfgManager)
	handlers.RegisterIdentityProxyRoutes(apiGroup, a.envCfg, a.cfgManager)
	apiGroup.Get("/network/interfaces", handlers.NetworkInterfaces())
	apiGroup.Post("/network/diagnose", handlers.DiagnoseNetwork(a.cfgManager))
	discoveryModelFetchers := buildChannelDiscoveryModelFetchers(a.cfgManager)
	apiGroup.Get("/console/status", handlers.ConsoleStatus(a.envCfg, a.cfgManager))
	apiGroup.Get("/console/codex-auto-review", handlers.GetCodexAutoReviewMapping(a.cfgManager))
	apiGroup.Put("/console/codex-auto-review", handlers.PutCodexAutoReviewMapping(a.cfgManager))
	apiGroup.Post("/copilot/oauth/device/code", copilot.RequestDeviceCode())
	apiGroup.Post("/copilot/oauth/token", copilot.PollAccessToken())
	apiGroup.Post("/copilot/oauth/verify", copilot.VerifyToken())

	// 预置数据（订阅来源分类等），前端表单选项来源；独立于 autopilot 开关。
	apiGroup.Get("/presets", presetstore.Handler(presetstore.Default()))
	apiGroup.Get("/presets/status", presetstore.StatusHandler(a.presetUpdater))

	// 配额真相快照：全部渠道的来源分级/headroom/窗口重置时间/饱和状态。
	// autopilot 关闭时 quotaManager 为 nil，返回空列表（fail-open）。
	apiGroup.Get("/quota/channels", handlers.GetQuotaChannels(a.quotaManager))

	apiGroup.Post("/channel-discovery", handlers.ChannelDiscoveryWithModelFetchers(a.cfgManager, discoveryModelFetchers))
	// 快速探活：仅探一个真实模型以定 primaryKind，不做全量协议/能力探测。
	apiGroup.Post("/channel-discovery-fast", handlers.ChannelDiscoveryFast(a.cfgManager))

	a.registerChannelRoutes(apiGroup)
	logicalchannels.RegisterRoutes(apiGroup, a.cfgManager, a.channelScheduler)
	channelsv2.RegisterRoutes(apiGroup, a.cfgManager, a.channelScheduler)
	a.registerAutopilotRoutes(apiGroup)
	apiGroup.Get("/racing/config", func(c fiber.Ctx) error {
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"enabled": a.cfgManager.GetRacingEnabled()})
		return nil
	})
	apiGroup.Put("/racing/config", func(c fiber.Ctx) error {
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil || req.Enabled == nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "无效的请求体，需提供 enabled 布尔值"})
			return nil
		}
		if err := a.cfgManager.SetRacingEnabled(*req.Enabled); err != nil {
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": "保存竞速配置失败"})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"enabled": *req.Enabled})
		return nil
	})

	// 熔断器运行时设置
	apiGroup.Get("/settings/circuit-breaker", handlers.GetCircuitBreaker(a.messagesMetricsManager.GetCircuitBreakerConfig, a.envCfg))
	apiGroup.Put("/settings/circuit-breaker", handlers.SetCircuitBreaker(a.cfgManager))

	// 会话调度看板 API
	convDeps := &handlers.ConversationHandlerDeps{
		Tracker:          a.conversationTracker,
		OverrideManager:  a.overrideManager,
		ChannelScheduler: a.channelScheduler,
		ConfigManager:    a.cfgManager,
	}
	apiGroup.Get("/conversations", handlers.GetConversations(convDeps))
	apiGroup.Post("/conversations/:id/override", handlers.SetConversationOverride(convDeps))
	apiGroup.Delete("/conversations/:id/override", handlers.RemoveConversationOverride(convDeps))
	apiGroup.Get("/conversations/settings", handlers.GetConversationSettings(convDeps))
	apiGroup.Put("/conversations/settings", handlers.UpdateConversationSettings(convDeps))

	apiGroup.Get("/metrics/prometheus", a.monitoring.Serve)
	apiGroup.Get("/reports/requests", handlers.GetRequestHistory(a.metricsStore))
	apiGroup.Get("/reports/request-timeline/:id", a.monitoring.ServeDiagnostic)
	// Phase 4 Item 2: 成本报表 API（按 user/model/key 分组聚合）
	apiGroup.Get("/reports/cost", handlers.GetCostReport(&handlers.CostReportDeps{
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
	apiGroup.Post("/channels/export", handlers.ExportChannels(a.envCfg, a.cfgManager))
	apiGroup.Get("/channels/export", handlers.ExportAllChannels(a.envCfg, a.cfgManager))
	apiGroup.Post("/channels/import", handlers.ImportChannels(a.cfgManager))
	apiGroup.Post("/channels/import/confirm", handlers.ImportChannelsConfirm(a.cfgManager))
	apiGroup.Get("/channels/templates", handlers.GetChannelTemplates())
	apiGroup.Get("/channels/provider-templates", handlers.GetProviderTemplates())
}
