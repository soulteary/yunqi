package main

import (
	"context"
	"log"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/autopilot"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/eventbus"
	"github.com/soulteary/yunqi/internal/handlers"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/presetstore"
	"github.com/soulteary/yunqi/internal/racing"
	"github.com/soulteary/yunqi/internal/scheduler"
)

func (a *application) initScheduler() {
	a.channelScheduler = scheduler.NewChannelScheduler(a.cfgManager, a.messagesMetricsManager, a.responsesMetricsManager, a.geminiMetricsManager, a.chatMetricsManager, a.imagesMetricsManager, a.traceAffinityManager, a.urlManager, a.vectorsMetricsManager)
	a.channelScheduler.SetRateLimitManager(a.rateLimitManager)
	log.Printf("[Scheduler-Init] 多渠道调度器已初始化 (失败率阈值: %.0f%%, 滑动窗口: %d, 连续失败阈值: %d)",
		a.messagesMetricsManager.GetFailureThreshold()*100, a.messagesMetricsManager.GetWindowSize(), a.messagesMetricsManager.GetConsecutiveRetryableFailuresThreshold())
}

func (a *application) configureEventBus() {
	// Phase B.1：创建跨模块事件总线并注入 ConfigManager + 6 个 MetricsManager + autopilot Manager。
	// 可选依赖：未注入时所有 publish no-op，系统行为不变。
	{
		a.stateEventBus = eventbus.NewBus()
		a.cfgManager.SetEventBus(a.stateEventBus)
		a.messagesMetricsManager.SetEventBus(a.stateEventBus)
		a.responsesMetricsManager.SetEventBus(a.stateEventBus)
		a.geminiMetricsManager.SetEventBus(a.stateEventBus)
		a.chatMetricsManager.SetEventBus(a.stateEventBus)
		a.imagesMetricsManager.SetEventBus(a.stateEventBus)
		a.vectorsMetricsManager.SetEventBus(a.stateEventBus)
		presetstore.Default().SetEventBus(a.stateEventBus)
		handlers.SetCapabilityTestEventBus(a.stateEventBus)
		if a.autopilotManager != nil {
			var stateStore *autopilot.StateEventStore
			if a.autopilotDB != nil {
				var storeErr error
				stateStore, storeErr = autopilot.NewStateEventStoreWithDB(a.autopilotDB)
				if storeErr != nil {
					log.Printf("[EventBus] 警告: 初始化 StateEventStore 失败: %v", storeErr)
					stateStore = nil
				}
			}
			a.autopilotManager.WireEventBus(a.stateEventBus, stateStore)
			if stateStore != nil {
				log.Printf("[EventBus] 跨模块事件总线已注入 (config + 6 metrics + autopilot manager + StateEventStore)")
			} else {
				log.Printf("[EventBus] 跨模块事件总线已注入 (config + 6 metrics + autopilot manager)")
			}
		}
	}
}

func (a *application) configureSchedulerPolicies() {
	// 通过 CandidateFilter 回调注入 Autopilot SmartRouter 的渠道级过滤与重排逻辑。
	// Kill Switch 启用时不注入 filter，恢复调度器默认行为。
	if a.autopilotManager != nil && a.autopilotManager.SmartRouter() != nil {
		sr := a.autopilotManager.SmartRouter()
		a.channelScheduler.SetCandidateFilterProvider(func(ctx context.Context, kind scheduler.ChannelKind, model string) (scheduler.CandidateFilterFunc, scheduler.CandidateSelectionObserver) {
			profile, ok := autopilot.RequestProfileFromContext(ctx)
			if !ok {
				profile = autopilot.BuildRequestProfile(autopilot.RequestProfileFeatures{
					Model:       model,
					ChannelKind: string(kind),
					Operation:   "completion",
				})
			}
			profile.Model = model
			profile.ChannelKind = string(kind)
			return sr.CandidateFilterForWithActual(&profile)
		})
		log.Printf("[Scheduler-Init] SmartRouter Autopilot filter 已注册")

		// 配额管理器注入 scheduler（饱和沉底排序用）
		if a.autopilotManager != nil && a.autopilotManager.SmartRouter() != nil {
			if qm := a.autopilotManager.SmartRouter().QuotaManager(); qm != nil {
				a.channelScheduler.SetQuotaManager(qm)
			}
		}
	}

	// EndpointAttemptPolicy 注入 + FastDecay 通知 + L2 探测 + 限速应用。
	// 注册 endpoint policy hook：handlers 层 TryUpstreamWithAllKeys 调用时自动获取 policy；
	// Kill Switch 启用时返回 nil，不注入策略。
	if a.autopilotManager != nil && a.autopilotManager.SmartRouter() != nil {
		sr := a.autopilotManager.SmartRouter()
		profileStore := sr.ProfileStore()
		traceStore := sr.TraceStore()
		fastDecayScorer := a.autopilotManager.FastDecayScorer()
		if traceStore != nil {
			common.SetRoutingOutcomeRecorderHook(func(traceUID string, outcome autopilot.RoutingOutcome) {
				if err := traceStore.RecordOutcome(traceUID, outcome); err != nil {
					log.Printf("[Autopilot-Outcome] 警告: trace=%s 终态记录失败: %v", traceUID, err)
				}
			})
			// endpoint 尝试摘要记录器：每次上游尝试向 trace 追加安全摘要
			common.SetAttemptRecorderHook(func(traceUID string, attempt autopilot.EndpointAttemptSummary) {
				traceStore.AppendEndpointAttempt(traceUID, attempt)
			})
			// scheduler 裁决记录器：选择完成后把过滤阶段/跳过明细附加到 trace
			common.SetSchedulerDecisionHook(func(traceUID string, trace *scheduler.SelectionTrace) {
				traceStore.AttachSchedulerDecision(traceUID, autopilot.NormalizeSelectionTrace(trace))
			})
		}

		// 场景预设配置读取器：请求画像构建时解析 X-Routing-Scenario 头与全局场景模式
		common.SetScenarioConfigProvider(func() config.ScenarioRoutingConfig {
			return a.cfgManager.GetAutopilotRouting().Scenario
		})

		// endpoint policy hook：为每个请求构建 EndpointAttemptPolicy
		common.SetEndpointPolicyProviderHook(func(c fiber.Ctx, model string, upstream *config.UpstreamConfig) *autopilot.EndpointAttemptPolicy {
			autopilotCfg := a.cfgManager.GetAutopilotRouting()
			if autopilotCfg.KillSwitch {
				return nil
			}
			mode := autopilot.RoutingModeAuto
			req := autopilot.BuildRequestProfile(autopilot.RequestProfileFeatures{Model: model})
			if c != nil && httpbridge.Request(c) != nil {
				if profile, ok := autopilot.RequestProfileFromContext(httpbridge.Request(c).Context()); ok {
					req = profile
					req.Model = model
				}
			}
			deps := autopilot.EndpointPolicyDeps{
				ProfileStore:  profileStore,
				FastDecay:     fastDecayScorer,
				TraceStore:    traceStore,
				ModelResolver: a.autopilotManager.ModelResolver(),
				GetRoutingCfg: func() config.AutopilotRoutingConfig { return a.cfgManager.GetAutopilotRouting() },
				APIKeyConfigs: config.NormalizeAPIKeyConfigsForView(*upstream),
			}
			return autopilot.BuildEndpointPolicy(deps, &req, mode)
		})

		// FastDecay 通知 hook：请求成功/失败时实时更新 FastDecayScorer
		common.SetNotifyEndpointResultHook(func(endpointUID string, success bool) {
			if fastDecayScorer != nil && endpointUID != "" {
				fastDecayScorer.RecordResult(endpointUID, success)
			}
		})
		log.Printf("[Autopilot-Init] EndpointAttemptPolicy hook + FastDecay notify hook 已注册")
	}

	// RateLimitApplier：将发现的限速建议应用到运行态 limiter
	if a.autopilotManager != nil && a.rateLimitManager != nil {
		rlApplier := autopilot.NewRateLimitApplier(
			a.autopilotManager.RateLimitDiscoverer(),
			a.rateLimitManager,
			func() config.AutopilotRoutingConfig { return a.cfgManager.GetAutopilotRouting() },
			a.envCfg.QuietPollingLogs,
		)
		a.autopilotManager.SetRateLimitApplier(rlApplier)
		a.autopilotManager.RefreshRateLimitMappings()
		// 配置变更时同步刷新 endpoint mapping，并立即唤醒 apply worker。
		// 这会使 kill switch、Enabled=false 或模式切换到 shadow/off 立即清理
		// 已注入 discovered RPM，而不等待 30 秒 ticker 或 5 分钟 collectAll。
		a.cfgManager.RegisterOnConfigChange(func(_ config.Config) {
			a.autopilotManager.RefreshRateLimitMappings()
			a.autopilotManager.RequestRateLimitApply()
		})
	}

	// Phase 4 Item 4: 用量画像记录 hook（渠道推荐用）。
	// 请求成功完成后（主响应已返回客户端之后）记录 proxyKeyMask -> channelUID 归因，
	// 纯观测性累积，不参与任何调度/候选过滤决策。
	if a.autopilotManager != nil {
		common.SetUsagePatternRecorderHook(func(proxyKeyMask, channelKind, channelUID, model string, domain autopilot.TaskDomain) {
			a.autopilotManager.RecordUsagePattern(proxyKeyMask, channelKind, channelUID, model, domain)
		})
	}
}

func (a *application) configureModelRouting() {
	// Phase 3B-2: ModelSupportResolver 注入（无条件注册，安全门控在 ResolveModelSupport 内部）。
	// 调度器候选筛选时调用，AutoManaged 渠道 + 三条件门控通过才走 ModelResolver，否则回退 ExplainModelSupport。
	if a.autopilotManager != nil {
		a.channelScheduler.SetModelSupportResolverProvider(func(ctx context.Context, kind scheduler.ChannelKind, upstream *config.UpstreamConfig, model string) (bool, string, string, string) {
			if profile, ok := autopilot.RequestProfileFromContext(ctx); ok {
				profile.Model = model
				profile.ChannelKind = string(kind)
				return a.autopilotManager.ResolveModelSupportWithFloor(
					string(kind),
					upstream,
					model,
					autopilot.BuildCapabilityFloorFromRequestProfile(&profile),
				)
			}
			return a.autopilotManager.ResolveModelSupport(string(kind), upstream, model)
		})
		log.Printf("[Autopilot-Init] ModelSupportResolver 已注册到调度器")
	}

	// 上下文有效窗口解析注入：scheduler 的上下文过滤不再只信注册表声明，
	// 而是合成学习证据（成功实证放宽棘轮 / models API 声明 / 实测 400 收紧）。
	// 渠道渐进扩容（200K→272K→372K→1M）由此自动跟进，注册表滞后不再锁死长对话。
	a.channelScheduler.SetContextWindowResolverProvider(func(channelUID string, kind scheduler.ChannelKind, actualModel string, registryWindow int) (int, int) {
		cache := config.SharedChannelCompatCache()
		if cache == nil || channelUID == "" || actualModel == "" {
			return registryWindow, 0
		}
		declared, _ := cache.MinContextLimitForChannelModel(channelUID, actualModel)
		return cache.EffectiveContextWindow(channelUID, string(kind), actualModel, registryWindow), declared
	})
	log.Printf("[Autopilot-Init] ContextWindowResolver 已注册到调度器")

	// 溢出跨协议重定向：同协议候选（含试探档）全灭时，从四类协议渠道池
	// 按质量档注入能承载的替代模型候选（用户拍板"完全跨协议"），发送层
	// 复用联邦改写与协议转换，响应头 X-CCX-Model-Redirect 标注。
	a.channelScheduler.SetOverflowCandidateProvider(func(ctx context.Context, kind scheduler.ChannelKind, model string, inputTokens int) []scheduler.ChannelInfo {
		return a.autopilotManager.OverflowRedirectCandidates(ctx, kind, model, inputTokens)
	})
	log.Printf("[Autopilot-Init] OverflowCandidateProvider 已注册")
}

func (a *application) configureRacing() {
	// 竞速（影子请求）配置 API + 编排依赖注入。
	// 候选缓存挂在 ABTestSampler 的 SmartRouter 排名回调上；采样器未初始化时
	// 竞速仅走调度器路由级回退。
	var racingCandidateProvider func(model, channelKind string) []autopilot.RoutingCandidate
	if sampler := a.autopilotManager.ABTestSampler(); sampler != nil {
		cache := sampler.CandidateCache()
		racingCandidateProvider = cache.Get
	}
	common.SetRacingHub(&common.RacingHub{
		Registry:          racing.NewRegistry(),
		Sem:               racing.NewSemaphore(racing.MaxConcurrentShadows()),
		CandidateProvider: racingCandidateProvider,
	})
}
