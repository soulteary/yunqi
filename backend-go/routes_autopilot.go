package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/autopilot"
	"github.com/soulteary/yunqi/internal/handlers"
	"github.com/soulteary/yunqi/internal/scheduler"
)

func (a *application) registerAutopilotRoutes(apiGroup fiber.Router) {
	// 健康中心 API（Phase 1 shadow/read-only）
	if a.autopilotManager != nil {
		autopilot.RegisterRoutes(apiGroup, a.autopilotManager)
		// 画像覆盖率诊断只读 API（Task 7 覆盖率门槛，手工映射表退役前验证用）
		autopilot.RegisterProfileCoverageRoutes(apiGroup, a.autopilotManager)

		// 订阅中心 API
		autopilot.RegisterSubscriptionRoutes(apiGroup, a.autopilotManager.SubscriptionStore(), a.autopilotManager.SubscriptionRefreshWorker(), a.newApiSyncService)
		autopilot.RegisterKeyMultiplierRoutes(apiGroup, a.cfgManager)
		autopilot.RegisterCostRoutes(apiGroup, a.cfgManager)
		// §8.5.1：new-api 订阅集成 API（校验 + 完整 provision 流程）
		autopilot.RegisterNewApiSubscriptionRoutes(apiGroup, &autopilot.NewApiRouteDeps{
			Store:       a.autopilotManager.SubscriptionStore(),
			CfgManager:  a.cfgManager,
			Runner:      a.autoDiscoveryRunner,
			SyncService: a.newApiSyncService,
		})
		// new-api 多账号管理 API
		autopilot.RegisterSubscriptionAccountRoutes(apiGroup, &autopilot.NewApiRouteDeps{
			Store:       a.autopilotManager.SubscriptionStore(),
			CfgManager:  a.cfgManager,
			Runner:      a.autoDiscoveryRunner,
			SyncService: a.newApiSyncService,
		})
		// 本地 Runtime API
		autopilot.RegisterLocalRuntimeRoutes(apiGroup, a.autopilotManager.LocalRuntimeStore())
		// 手动意图 API
		autopilot.RegisterManualIntentRoutes(apiGroup, a.autopilotManager.ManualIntentStore())
		// 本地任务模板 API
		autopilot.RegisterTaskTemplateRoutes(apiGroup, a.autopilotManager.TaskTemplateStore())
		// 驾驶舱只读聚合 API
		autopilot.RegisterCockpitRoutes(apiGroup, a.autopilotManager)
		// Advisor shadow 决策记录 API
		autopilot.RegisterAdvisorRoutes(apiGroup, a.autopilotManager.AdvisorDecisionStore())
		// Phase 4 Item 4: 渠道推荐只读 API
		autopilot.RegisterRecommendationRoutes(apiGroup, a.autopilotManager)

		// Phase 4 Item 8: A/B 测试结果 + 紧急停止 API
		autopilot.RegisterRoutingEvaluationRoutes(apiGroup, a.autopilotManager.ABTestStore())
		if a.autopilotManager.ABTestSampler() != nil && a.autopilotManager.ABTestStore() != nil {
			autopilot.RegisterABTestRoutes(apiGroup, &autopilot.ABTestDeps{
				Sampler:    a.autopilotManager.ABTestSampler(),
				Store:      a.autopilotManager.ABTestStore(),
				CfgManager: a.cfgManager,
			})
		}
		// 路由追踪 API
		if a.autopilotManager.TraceStore() != nil {
			autopilot.RegisterTraceRoutes(apiGroup, a.autopilotManager.TraceStore())
		}
		// SmartRouter dry-run API
		if a.autopilotManager.SmartRouter() != nil {
			autopilot.RegisterDryRunRoutes(apiGroup, a.autopilotManager.SmartRouter())
			// 路由预演 API（请求体直喂 + 两层对齐）
			autopilot.RegisterRoutePreviewRoutes(apiGroup, a.autopilotManager.SmartRouter(), a.channelScheduler)
		}

		// 渠道兼容性能力记忆查看/清除（工具调用、安全分类等自学习结论）
		handlers.RegisterCompatCacheRoutes(apiGroup)

		// Phase 2 第三批：自动托管 API（复用上方已构造的 autoDiscoveryRunner）
		autopilot.RegisterAutoManagedRoutes(apiGroup, &autopilot.AutoManagedDeps{
			CfgManager:          a.cfgManager,
			Runner:              a.autoDiscoveryRunner,
			RateLimitDiscoverer: a.autopilotManager.RateLimitDiscoverer(),
			ResetChannelMetrics: func(kind string, index int) {
				switch scheduler.ChannelKind(kind) {
				case scheduler.ChannelKindMessages, scheduler.ChannelKindChat, scheduler.ChannelKindResponses,
					scheduler.ChannelKindGemini, scheduler.ChannelKindImages, scheduler.ChannelKindVectors:
					a.channelScheduler.ResetChannelMetrics(index, scheduler.ChannelKind(kind))
				}
			},
		})

		// Phase 2 第三批：智能路由配置 API
		autopilot.RegisterRoutingConfigRoutes(apiGroup, &autopilot.RoutingConfigDeps{
			CfgManager: a.cfgManager,
			TraceStore: a.autopilotManager.TraceStore(),
		})
	}

}
