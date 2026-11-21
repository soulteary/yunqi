package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/soulteary/yunqi/internal/autopilot"
	"github.com/soulteary/yunqi/internal/balanceprobe"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/handlers/chat"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/handlers/gemini"
	"github.com/soulteary/yunqi/internal/handlers/images"
	"github.com/soulteary/yunqi/internal/handlers/messages"
	"github.com/soulteary/yunqi/internal/handlers/responses"
	"github.com/soulteary/yunqi/internal/handlers/vectors"
	"github.com/soulteary/yunqi/internal/healthcheck"
	"github.com/soulteary/yunqi/internal/httpclient"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/presetstore"
	"github.com/soulteary/yunqi/internal/scheduler"
)

func (a *application) startPresetUpdater() {
	// 远程预置更新：启动时先尝试恢复已校验磁盘缓存，再由后台 worker 异步检查文档站。
	// 网络/缓存失败不阻断服务，始终保留编译期 embedded fallback。
	a.presetUpdater = presetstore.NewPresetUpdater(presetstore.Default(), presetstore.UpdaterConfig{
		Enabled:  a.envCfg.PresetUpdateEnabled,
		IndexURL: a.envCfg.PresetUpdateURL,
		Interval: time.Duration(a.envCfg.PresetUpdateIntervalMinutes) * time.Minute,
		CacheDir: a.paths.PresetCacheDir,
	})
	if err := a.presetUpdater.LoadCacheAtStartup(); err != nil && !os.IsNotExist(err) {
		log.Printf("[PresetUpdater-Cache] 缓存不可用，继续使用内置预置: %v", err)
	}
	a.presetUpdater.Start(context.Background())
}

func (a *application) startAutopilotWorkers() {
	// L2 ProbeWorker：按配置门控启动（默认关闭）
	if a.autopilotManager != nil {
		autopilotCfg := a.cfgManager.GetAutopilotRouting()
		if autopilotCfg.HealthCheck.L2ProbeEnabled {
			probeWorker := autopilot.NewProbeWorker(
				a.autopilotManager.ProfileStore(),
				autopilot.ProbeWorkerConfig{
					QuietLogs:              a.envCfg.QuietPollingLogs,
					ProbeRecoveryThreshold: autopilotCfg.HealthCheck.ProbeRecoveryThreshold,
				},
			)
			probeWorker.SetAPIKeyResolver(a.autopilotManager.ResolveAPIKey)
			probeWorker.SetClientResolver(func(uid string, timeout time.Duration) (*http.Client, bool) {
				up := a.cfgManager.GetUpstreamByUID("", uid)
				if up == nil {
					return nil, false
				}
				opts := httpclient.ClientOptions{Timeout: timeout, ProxyURL: up.ProxyURL, ProxyPreferDirect: up.ProxyPreferDirect, Insecure: up.InsecureSkipVerify, OutboundNetwork: up.OutboundNetwork}
				if _, err := httpclient.ResolveOutboundPlan(opts); err != nil {
					return nil, false
				}
				return httpclient.GetManager().GetClient(opts), true
			})
			a.autopilotManager.SetProbeWorker(probeWorker)
			log.Printf("[Autopilot-Init] L2 ProbeWorker 已创建 (将在 StartWorker 时启动)")
		}
	}

	// Phase 4 Item 6: SubscriptionRefreshWorker：按配置门控启动（默认关闭）
	if a.autopilotManager != nil {
		autopilotCfg := a.cfgManager.GetAutopilotRouting()
		if autopilotCfg.SubscriptionAutoRefresh.Enabled {
			refreshWorker := autopilot.NewSubscriptionRefreshWorker(
				a.autopilotManager.SubscriptionStore(),
				nil, // 使用默认 fetcher 注册表（OpenAI/Anthropic/Google）
				autopilot.SubscriptionRefreshWorkerConfig{
					RefreshInterval: time.Duration(autopilotCfg.SubscriptionAutoRefresh.RefreshIntervalHours) * time.Hour,
					DailyBudget:     autopilotCfg.SubscriptionAutoRefresh.DailyBudget,
					RefreshTimeout:  time.Duration(autopilotCfg.SubscriptionAutoRefresh.RequestTimeoutSeconds) * time.Second,
					QuietLogs:       a.envCfg.QuietPollingLogs,
				},
				func() bool { return a.cfgManager.GetAutopilotRouting().SubscriptionAutoRefresh.Enabled },
			)
			// 余额刷新结果接入配额管理器（provider_api 级，最高可信度来源）：
			// SmartRouter 余量评分与 scheduler 饱和沉底由此获得真实余额数据。
			if qm := a.autopilotManager.SmartRouter().QuotaManager(); qm != nil {
				refreshWorker.SetQuotaManager(qm)
			}
			a.autopilotManager.SetSubscriptionRefreshWorker(refreshWorker)
			log.Printf("[Autopilot-Init] SubscriptionRefreshWorker 已创建 (将在 StartWorker 时启动)")
		}
	}

	if a.autopilotManager != nil {
		a.autopilotManager.StartWorker(context.Background())
		log.Printf("[Autopilot-Init] 健康中心已初始化 (DB: %s, 间隔: 5分钟)", a.paths.AutopilotDBPath)
	}
}

func (a *application) startHealthChecks() {
	// 渠道保活验证（L1 带 key 拉上游模型列表）：依赖指标持久化存储 key_health 表
	if a.metricsStore != nil {
		a.healthCheckManager = healthcheck.NewManager(
			func() config.Config { return a.cfgManager.GetConfig() },
			a.metricsStore,
			// 鉴权失败拉黑：交 ConfigManager.BlacklistKeyWithRecoverAt
			func(channelType string, channelIndex int, apiKey, reason, message, recoverAt string) {
				if err := a.cfgManager.BlacklistKeyWithRecoverAt(healthCheckAPIType(channelType), channelIndex, apiKey, reason, message, recoverAt); err != nil {
					log.Printf("[HealthCheck-Blacklist] 警告: 拉黑 Key 失败: %v", err)
				}
			},
			// 失败喂熔断：按渠道类型喂对应指标管理器，并写入渠道日志（保活验证失败此前对渠道日志不可见）
			func(channelType string, channelIndex int, baseURL, apiKey, serviceType, model, detail string) {
				kind := scheduler.ChannelKind(channelType)
				normalizedServiceType := scheduler.NormalizedMetricsServiceType(kind, serviceType)
				a.channelScheduler.RecordFailure(baseURL, apiKey, normalizedServiceType, model, kind)

				channelName := ""
				cfg := a.cfgManager.GetConfig()
				if upstreams := healthcheck.UpstreamsFor(&cfg, channelType); channelIndex >= 0 && channelIndex < len(upstreams) {
					channelName = upstreams[channelIndex].Name
				}
				metricsKey := metrics.GenerateMetricsIdentityKey(baseURL, apiKey, normalizedServiceType)
				common.RecordChannelLogWithSource(
					a.channelScheduler.GetChannelLogStore(kind),
					metricsKey,
					channelIndex,
					model, "", 0, 0, false,
					apiKey, baseURL, detail, channelType,
					false,
					metrics.RequestSourceHealthCheck,
					channelName,
				)
			},
			healthcheck.Options{},
		)
		a.healthCheckManager.SetModelCircuitLookup(func(channelType string) *metrics.ModelCircuitTracker {
			var manager *metrics.MetricsManager
			switch channelType {
			case "messages":
				manager = a.channelScheduler.GetMessagesMetricsManager()
			case "chat":
				manager = a.channelScheduler.GetChatMetricsManager()
			case "responses":
				manager = a.channelScheduler.GetResponsesMetricsManager()
			case "gemini":
				manager = a.channelScheduler.GetGeminiMetricsManager()
			case "images":
				manager = a.channelScheduler.GetImagesMetricsManager()
			case "vectors":
				manager = a.channelScheduler.GetVectorsMetricsManager()
			}
			if manager == nil {
				return nil
			}
			return manager.ModelCircuit()
		})
		// 注册六类渠道的 L1 fetcher（复用各渠道 GetChannelModels handler 的薄包装）
		a.healthCheckManager.RegisterL1Fetcher("messages", healthCheckL1Fetcher(messages.GetChannelModels(a.cfgManager)))
		a.healthCheckManager.RegisterL1Fetcher("chat", healthCheckL1Fetcher(chat.GetChannelModels(a.cfgManager)))
		a.healthCheckManager.RegisterL1Fetcher("responses", healthCheckL1Fetcher(responses.GetChannelModels(a.cfgManager)))
		a.healthCheckManager.RegisterL1Fetcher("gemini", healthCheckL1Fetcher(gemini.GetChannelModels(a.cfgManager)))
		a.healthCheckManager.RegisterL1Fetcher("images", healthCheckL1Fetcher(images.GetChannelModels(a.cfgManager)))
		a.healthCheckManager.RegisterL1Fetcher("vectors", healthCheckL1Fetcher(vectors.GetChannelModels(a.cfgManager)))
		// 注入火山套餐 AFP 余额查询器，稀疏 L2 预算按剩余额度比例收紧，避免探测蚕食生产额度
		a.healthCheckManager.SetProbeUsageResolver(a.cfgManager)
		a.healthCheckManager.Start()
	} else {
		log.Printf("[HealthCheck-Init] 指标持久化不可用，渠道保活验证未启动")
	}
}

func (a *application) startBalanceProbes() {
	// 上游余额探测：周期/手动触发渠道 key 的上游余额查询（newapi_token/sub2api/newapi_billing），
	// 明细落 key_health（check_kind=balance），聚合写配额真相 provider_api 级参与调度。
	if a.metricsStore != nil {
		a.balanceProbeManager = balanceprobe.NewManager(
			func() config.Config { return a.cfgManager.GetConfig() },
			a.metricsStore,
			a.quotaManager,
			balanceprobe.Options{},
		)
		a.balanceProbeManager.Start()
	} else {
		log.Printf("[BalanceProbe-Init] 指标持久化不可用，上游余额探测未启动")
	}
}

func (a *application) startDiscoveryServices() {
	if a.autopilotManager == nil {
		return
	}
	// 自动托管与 new-api provision 共用同一个 discovery runner；
	// 在注册管理路由前完成持久化恢复及后台任务启动。
	a.autoDiscoveryRunner = autopilot.NewAutoDiscoveryRunner(a.autopilotManager.ProfileStore(), a.autopilotManager.EventHub())
	// Phase 3B-2：注入 ModelProfileStore，使自动发现时同步写入 model_profiles
	if mps := a.autopilotManager.ModelProfileStore(); mps != nil {
		a.autoDiscoveryRunner.ModelProfileStore = mps
	}
	// 注入共享事件总线；事件为只读观测，不影响调度。
	a.autoDiscoveryRunner.SetEventBus(a.stateEventBus)
	// 后台 discovery 任务落盘 + 断点续传：复用 ProfileStore 的 SQLite 连接，
	// 注入 taskStore 启用端点级 checkpoint；并在接受请求前恢复未完成 discovery。
	if ps := a.autopilotManager.ProfileStore(); ps != nil && ps.DB() != nil {
		discoveryTaskStore, err := autopilot.NewDiscoveryTaskStoreWithDB(ps.DB())
		if err != nil {
			log.Printf("[Autopilot-DiscoveryTaskStore] 初始化失败，降级为纯内存 discovery: %v", err)
		} else {
			a.autoDiscoveryRunner.SetTaskStore(discoveryTaskStore)
			a.autoDiscoveryRunner.ResumeIncompleteDiscoveries(a.cfgManager)
			a.autoDiscoveryRunner.StartModelRefreshLoop(a.cfgManager)
		}
	}

	a.newApiSyncService = autopilot.NewNewApiSubscriptionSyncService(autopilot.NewApiSubscriptionSyncServiceDeps{
		Store: a.autopilotManager.SubscriptionStore(), CfgManager: a.cfgManager, Runner: a.autoDiscoveryRunner,
		QuietLogs: a.envCfg.QuietPollingLogs,
		Enabled:   func() bool { return a.cfgManager.GetAutopilotRouting().SubscriptionAutoRefresh.Enabled },
	})
	// new-api 余额同步成功后接入配额真相体系（configured 级；
	// provider_api 级仍由 SubscriptionRefreshWorker 的 BillingAPIKey 路径负责）。
	if qm := a.autopilotManager.SmartRouter().QuotaManager(); qm != nil {
		a.newApiSyncService.SetQuotaManager(qm)
	}
	a.newApiSyncService.SyncAllNewAPIAsync(context.Background())
	a.newApiSyncService.Start(context.Background())
}

func (a *application) stopBackgroundTasks() {
	// 停止远程预置更新器（取消进行中的 HTTP 请求并等待 worker 退出）。
	a.presetUpdater.Stop()
	log.Println("[PresetUpdater-Shutdown] 预置更新器已安全关闭")

	// 停止渠道保活验证（等待 worker 池排空后再关指标存储）
	if a.healthCheckManager != nil {
		a.healthCheckManager.Stop()
		log.Println("[HealthCheck-Shutdown] 渠道保活验证已安全关闭")
	}

	// 停止后台 discovery runner：取消未完成任务并停止 GC。
	// 已持久化的 running 状态与 checkpoint 保留，下次启动经 ResumeIncompleteDiscoveries 续传。
	if a.autoDiscoveryRunner != nil {
		a.autoDiscoveryRunner.Stop()
		log.Println("[AutoDiscovery-Shutdown] 后台发现执行器已停止")
	}

	// 停止 new-api 订阅周期性余额/倍率同步。
	if a.newApiSyncService != nil {
		a.newApiSyncService.Stop()
		log.Println("[NewApiSubscriptionSync-Shutdown] 周期性同步已停止")
	}

	// 关闭指标持久化存储
	if a.metricsStore != nil {
		if err := a.metricsStore.Close(); err != nil {
			log.Printf("[Metrics-Shutdown] 警告: 关闭指标存储时发生错误: %v", err)
		} else {
			log.Println("[Metrics-Shutdown] 指标存储已安全关闭")
		}
	}

	// 关闭对话追踪器（flush 持久化状态）
	a.conversationTracker.Stop()
	log.Println("[Conversation-Shutdown] 对话追踪器已安全关闭")

	// 停止 Autopilot 健康中心（flush 画像 + 关闭 SQLite）
	if a.autopilotManager != nil {
		if err := a.autopilotManager.Close(); err != nil {
			log.Printf("[Autopilot-Shutdown] 警告: 关闭健康中心时发生错误: %v", err)
		} else {
			log.Println("[Autopilot-Shutdown] 健康中心已安全关闭")
		}
	}

	// 停止调度器后台 reaper
	a.channelScheduler.Stop()

	// 停止限速器后台清理协程
	a.rateLimitManager.Stop()

	close(a.scheduledRecoveryStop)
}
