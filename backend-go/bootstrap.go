package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/soulteary/yunqi/internal/autopilot"
	"github.com/soulteary/yunqi/internal/balanceprobe"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/conversation"
	"github.com/soulteary/yunqi/internal/errutil"
	"github.com/soulteary/yunqi/internal/eventbus"
	"github.com/soulteary/yunqi/internal/handlers"
	"github.com/soulteary/yunqi/internal/healthcheck"
	"github.com/soulteary/yunqi/internal/logger"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/presetstore"
	"github.com/soulteary/yunqi/internal/quota"
	"github.com/soulteary/yunqi/internal/ratelimit"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/telemetry"
	"github.com/soulteary/yunqi/internal/thinkingcache"
	"github.com/soulteary/yunqi/internal/warmup"
)

type application struct {
	monitoring    *telemetry.Registry
	envCfg        *config.EnvConfig
	paths         runtimePaths
	cfgManager    *config.ConfigManager
	presetUpdater *presetstore.PresetUpdater

	sessionManager *session.SessionManager

	metricsStore            *metrics.SQLiteStore
	messagesMetricsManager  *metrics.MetricsManager
	responsesMetricsManager *metrics.MetricsManager
	geminiMetricsManager    *metrics.MetricsManager
	chatMetricsManager      *metrics.MetricsManager
	imagesMetricsManager    *metrics.MetricsManager
	vectorsMetricsManager   *metrics.MetricsManager
	traceAffinityManager    *session.TraceAffinityManager
	rateLimitManager        *ratelimit.Manager
	urlManager              *warmup.URLManager

	autopilotManager      *autopilot.Manager
	autopilotDB           *sql.DB
	autoDiscoveryRunner   *autopilot.AutoDiscoveryRunner
	newApiSyncService     *autopilot.NewApiSubscriptionSyncService
	quotaManager          *quota.Manager
	channelScheduler      *scheduler.ChannelScheduler
	stateEventBus         *eventbus.Bus
	conversationTracker   *conversation.ConversationTracker
	overrideManager       *conversation.OverrideManager
	healthCheckManager    *healthcheck.Manager
	balanceProbeManager   *balanceprobe.Manager
	scheduledRecoveryStop chan struct{}
}

// runService is the composition root. Keep startup ordering explicit: configuration,
// state and policies first, workers next, routes and HTTP serving last.
func runService(cliOpts cliOptions) {
	// 加载环境变量
	if err := godotenv.Load(); err != nil {
		log.Println("没有找到 .env 文件，使用环境变量或默认值")
	}

	// 设置版本信息到 handlers 包
	handlers.SetVersionInfo(Version, BuildTime, GitCommit)

	// 注册渠道兼容性探测：让无上游报错信号的兼容项（reasoning_content 回传、空 text block 剥离）
	// 在首次遇到渠道-Key-模型组合时自动探测一次并记忆，无需用户手工点诊断按钮
	handlers.RegisterCompatProbeHook()

	// 初始化环境配置，并应用命令行运行时路径覆盖
	envCfg := config.NewEnvConfig()
	paths, err := resolveRuntimePaths(cliOpts, envCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析运行时路径失败: %v\n", err)
		os.Exit(2)
	}

	// 初始化日志系统（必须在其他初始化之前）
	logCfg := &logger.Config{
		LogDir:     paths.LogDir,
		LogFile:    envCfg.LogFile,
		MaxSize:    envCfg.LogMaxSize,
		MaxBackups: envCfg.LogMaxBackups,
		MaxAge:     envCfg.LogMaxAge,
		Compress:   envCfg.LogCompress,
		Console:    envCfg.LogToConsole,
	}
	if err := logger.Setup(logCfg); err != nil {
		log.Fatalf("初始化日志系统失败: %v", err)
	}

	cfgManager, err := config.NewConfigManager(paths.ConfigPath, paths.BackupDir)
	if err != nil {
		log.Fatalf("初始化配置管理器失败: %v", err)
	}
	defer errutil.IgnoreDeferred(cfgManager.Close)

	a := &application{envCfg: envCfg, paths: paths, cfgManager: cfgManager}
	a.configureGuardrails()
	a.startPresetUpdater()
	a.configureThinkingCache()
	defer errutil.IgnoreDeferred(thinkingcache.Close)
	a.configureSchedulerTuning()
	a.initSessionManager()
	a.initMetrics()
	a.initRateLimits()
	a.initURLManager()
	a.initAutopilot()
	a.initScheduler()
	a.configureEventBus()
	a.configureSchedulerPolicies()
	a.startAutopilotWorkers()
	a.configureModelRouting()
	a.initConversations()
	a.channelScheduler.Start()
	a.startHealthChecks()
	a.startBalanceProbes()
	a.startScheduledRecovery()
	// Discovery formerly started inside the route declarations. Complete the same
	// wiring before registering routes, so router construction itself starts no workers.
	a.startDiscoveryServices()
	a.configureRacing()
	a.serve(a.newRouter())
}
