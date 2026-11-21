package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/soulteary/yunqi/internal/autopilot"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/quota"
	"github.com/soulteary/yunqi/internal/ratelimit"
)

func (a *application) initAutopilot() {
	// 初始化 Autopilot 健康中心（Phase 1 shadow/read-only）
	{
		autopilotStore, apErr := autopilot.NewProfileStore(a.paths.AutopilotDBPath)
		if apErr != nil {
			log.Printf("[Autopilot-Init] 警告: 初始化 ProfileStore 失败: %v (健康中心将不可用)", apErr)
		} else {
			metricsAdapters := map[string]autopilot.MetricsProvider{
				"messages":  autopilot.NewMetricsManagerAdapter(a.messagesMetricsManager),
				"responses": autopilot.NewMetricsManagerAdapter(a.responsesMetricsManager),
				"gemini":    autopilot.NewMetricsManagerAdapter(a.geminiMetricsManager),
				"chat":      autopilot.NewMetricsManagerAdapter(a.chatMetricsManager),
				"images":    autopilot.NewMetricsManagerAdapter(a.imagesMetricsManager),
				"vectors":   autopilot.NewMetricsManagerAdapter(a.vectorsMetricsManager),
			}
			autopilotMetrics := autopilot.NewMetricsAdapterManager(metricsAdapters)
			mgr, mgrErr := autopilot.NewManager(autopilotStore, autopilotMetrics, a.cfgManager, autopilot.ManagerConfig{
				WorkerInterval: 5 * time.Minute,
				QuietLogs:      a.envCfg.QuietPollingLogs,
			})
			if mgrErr != nil {
				log.Printf("[Autopilot-Init] 警告: 初始化 Manager 失败: %v (健康中心将不可用)", mgrErr)
			} else {
				a.autopilotManager = mgr
				a.autopilotDB = autopilotStore.DB() // Phase B.1: 复用 SQLite 连接
				// 共享 ProfileStore：能力测试的探测范围对齐（画像 protocolModels 优先于内置通用清单）
				autopilot.SetSharedProfileStore(autopilotStore)

				// Phase 2: 创建 TraceStore（内存环形 + 可选 SQLite 落盘）
				traceStore, tsErr := autopilot.NewTraceStoreWithDB(autopilotStore.DB())
				if tsErr != nil {
					log.Printf("[Autopilot-Init] 警告: 初始化 TraceStore 失败: %v (路由追踪将不可用)", tsErr)
				} else {
					a.autopilotManager.SetTraceStore(traceStore)
					log.Printf("[Autopilot-Init] TraceStore 已初始化")
				}

				// 创建始终自动运行的 SmartRouter
				smartRouter := autopilot.NewSmartRouter(
					autopilotStore,
					a.autopilotManager.ManualIntentStore(),
					traceStore,
					a.cfgManager,
				)
				a.autopilotManager.SetSmartRouter(smartRouter)

				// 配额管理器：真相分级 + 懒重置饱和桶 + 响应头解析（§2 配额真相分级调度）
				a.quotaManager = quota.NewManager()
				smartRouter.SetQuotaManager(a.quotaManager)

				// configured 级生产接线（§7.3）：订阅画像的静态额度声明在配置变更
				// （热重载 / new-api reconcile 落盘 / 手工配置）时全量重放进配额体系。
				// 闭包延迟取 SubscriptionStore——此时订阅存储尚未就绪，回调只会在
				// 配置文件实际变更后触发。
				a.cfgManager.RegisterOnConfigChange(func(cfg config.Config) {
					if store := a.autopilotManager.SubscriptionStore(); store != nil {
						autopilot.SyncAllSubscriptionsQuotaAsConfigured(a.quotaManager, store, cfg)
					}
				})

				// Phase 2: 将 Advisor + LocalRuntimeStore 注入 SmartRouter
				a.autopilotManager.WireSmartRouter()
				log.Printf("[Autopilot-Init] SmartRouter advisor + localRuntimeStore 已注入")

				// 模型熔断探针：SmartRouter 精确模型运行期否决判定用
				// （按 channelKind 路由到对应 MetricsManager，镜像下方 healthCheck 的逐类 tracker 解析）。
				smartRouter.SetModelCircuitProbe(func(channelKind, channelUID, apiKey, model string) bool {
					var mgr *metrics.MetricsManager
					switch channelKind {
					case "messages":
						mgr = a.messagesMetricsManager
					case "responses":
						mgr = a.responsesMetricsManager
					case "gemini":
						mgr = a.geminiMetricsManager
					case "chat":
						mgr = a.chatMetricsManager
					case "images":
						mgr = a.imagesMetricsManager
					case "vectors":
						mgr = a.vectorsMetricsManager
					}
					if mgr == nil {
						return false
					}
					tracker := mgr.ModelCircuit()
					if tracker == nil {
						return false
					}
					return tracker.IsModelCircuitOpen(channelUID, metrics.ModelCircuitKeyHash(apiKey), model)
				})

				log.Printf("[Autopilot-Init] SmartRouter 已初始化 (Autopilot 自动运行)")

				// 注册限速信号回调：上游响应 → autopilot 限速发现器 + 时间桶
				// endpointUID 和 metricsKey 由 upstream_failover.go 在请求上下文中计算后传入
				// reason 携带 429 细分原因（如 account_rate_limit_exceeded），由
				// upstream_failover.go 读完 body 分类后传入，确保同一次 429 只通知一次
				ratelimit.SetUpstreamSignalCallback(func(channelUID, endpointUID, metricsKey, serviceType, channelName string, isStream bool, latencyMs int64, headers http.Header, statusCode int, reason string) {
					if a.autopilotManager.ObserveRateLimitSignal(endpointUID, 0, metricsKey, serviceType, channelName, isStream, latencyMs, headers, statusCode, reason) {
						a.autopilotManager.RequestRateLimitApply()
					}
					// 配额真相 response_headers 级接线（§2 配额真相分级调度）：
					// 与限速发现器共享同一观测回调，从响应头学习 token/request 余量，
					// 供 SmartRouter 评分与 scheduler 饱和沉底消费。accountUID 用
					// endpointUID，饱和桶按 endpoint（渠道+基地址+Key）粒度聚合。
					a.quotaManager.UpdateFromUpstreamSignal(channelUID, endpointUID, serviceType, headers)
				})

				// 限速发现器配置接线：从 AutopilotRouting.RateLimitDiscovery 映射到 Discoverer 参数。
				// 配置热重载时同步刷新；PassiveAimdEnabled=false 时仍展示 header 信号但不执行 AIMD。
				applyRateLimitDiscoveryConfig := func() {
					disc := a.autopilotManager.RateLimitDiscoverer()
					if disc == nil {
						return
					}
					rlCfg := a.cfgManager.GetAutopilotRouting().RateLimitDiscovery
					discCfg := autopilot.RateLimitDiscovererConfig{
						PassiveAimdEnabled: rlCfg.PassiveAimdEnabled,
					}
					if rlCfg.MinRpm > 0 {
						discCfg.MinRPM = rlCfg.MinRpm
					}
					if rlCfg.MaxAutoRpm > 0 {
						discCfg.MaxAutoRPM = rlCfg.MaxAutoRpm
					}
					if rlCfg.MaxAutoConcurrent > 0 {
						discCfg.MaxAutoConcurrent = rlCfg.MaxAutoConcurrent
					}
					if rlCfg.ConfidenceThreshold > 0 {
						discCfg.ConfidenceThreshold = rlCfg.ConfidenceThreshold
					}
					if rlCfg.IncreaseIntervalMinutes > 0 {
						discCfg.AIMDIncreaseInterval = time.Duration(rlCfg.IncreaseIntervalMinutes) * time.Minute
					}
					if rlCfg.IncreaseStepPercent > 0 {
						discCfg.AIMDIncreasePercent = float64(rlCfg.IncreaseStepPercent)
					}
					disc.UpdateConfig(discCfg)
				}
				applyRateLimitDiscoveryConfig()
				a.cfgManager.RegisterOnConfigChange(func(_ config.Config) {
					applyRateLimitDiscoveryConfig()
				})

				// Phase 4 Item 8: A/B 测试（低比例统计抽样双发）
				// 默认关闭（Enabled=false），需显式 opt-in。
				// 主请求路径完全不变：影子请求在主响应返回后异步发起。
				abTestStore, abErr := autopilot.NewABTestStoreWithDB(autopilotStore.DB())
				if abErr != nil {
					log.Printf("[Autopilot-Init] 警告: 初始化 ABTestStore 失败: %v (A/B 测试将不可用)", abErr)
				} else {
					a.autopilotManager.SetABTestStore(abTestStore)
					abTestSampler := autopilot.NewABTestSampler(abTestStore, func() autopilot.ABTestSamplerConfig {
						abCfg := a.cfgManager.GetAutopilotRouting().ABTest
						return autopilot.ABTestSamplerConfig{
							Enabled:                  abCfg.Enabled,
							SampleRatio:              abCfg.SampleRatio,
							MaxShadowRequestsPerHour: abCfg.MaxShadowRequestsPerHour,
							ShadowCandidateCount:     abCfg.ShadowCandidateCount,
						}
					})
					a.autopilotManager.SetABTestSampler(abTestSampler)
					// 将 SmartRouter 候选排名回调连接到 A/B 测试缓存
					if smartRouter != nil {
						smartRouter.SetOnCandidatesRanked(abTestSampler.OnCandidatesRanked())
					}
					// 注册代理成功后回调：主响应已返回客户端后异步发起影子请求
					// 影子请求在独立 goroutine 中执行，不阻塞主请求路径
					common.SetPostSuccessfulProxyHook(func(channelKind, model, channelUID string, statusCode int, latencyMs int64, bodyBytes []byte) {
						// 获取全局 KillSwitch 状态
						routingCfg := a.cfgManager.GetAutopilotRouting()
						killSwitchActive := routingCfg.KillSwitch
						if envKillSwitch := os.Getenv("AUTOPILOT_KILL_SWITCH"); envKillSwitch == "true" || envKillSwitch == "1" {
							killSwitchActive = true
						}
						if !abTestSampler.ShouldSample(killSwitchActive) {
							return
						}
						abTestSampler.ExecuteShadowRequest(
							context.Background(),
							a.cfgManager,
							bodyBytes,
							model,
							channelKind,
							channelUID,
							statusCode,
							latencyMs,
						)
					})
					log.Printf("[Autopilot-Init] ABTestSampler 已初始化 (默认关闭, 抽样率: 0.01)")
				}
			}
		}
	}
}
