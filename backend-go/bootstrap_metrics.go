package main

import (
	"log"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/session"
)

func (a *application) initMetrics() {
	// 初始化指标持久化存储（可选）
	if a.envCfg.MetricsPersistenceEnabled {
		var err error
		a.metricsStore, err = metrics.NewSQLiteStore(&metrics.SQLiteStoreConfig{
			DBPath:        a.paths.MetricsDBPath,
			RetentionDays: a.envCfg.MetricsRetentionDays,
		})
		if err != nil {
			log.Printf("[Metrics-Init] 警告: 初始化指标持久化存储失败: %v，将使用纯内存模式", err)
			a.metricsStore = nil
		}
	} else {
		log.Printf("[Metrics-Init] 指标持久化已禁用，使用纯内存模式")
	}

	// 初始化多渠道调度器（Messages、Responses、Gemini、Chat、Images 和 Vectors 使用独立的指标管理器）
	if a.metricsStore != nil {
		if err := a.metricsStore.MigrateMetricsKeysToIdentity(a.cfgManager.GetConfig()); err != nil {
			log.Fatalf("[Metrics-Migration] metrics key 迁移失败: %v", err)
		}
		a.messagesMetricsManager = metrics.NewMetricsManagerWithPersistence(
			a.envCfg.MetricsWindowSize, a.envCfg.MetricsFailureThreshold, a.metricsStore, "messages")
		a.responsesMetricsManager = metrics.NewMetricsManagerWithPersistence(
			a.envCfg.MetricsWindowSize, a.envCfg.MetricsFailureThreshold, a.metricsStore, "responses")
		a.geminiMetricsManager = metrics.NewMetricsManagerWithPersistence(
			a.envCfg.MetricsWindowSize, a.envCfg.MetricsFailureThreshold, a.metricsStore, "gemini")
		a.chatMetricsManager = metrics.NewMetricsManagerWithPersistence(
			a.envCfg.MetricsWindowSize, a.envCfg.MetricsFailureThreshold, a.metricsStore, "chat")
		a.imagesMetricsManager = metrics.NewMetricsManagerWithPersistence(
			a.envCfg.MetricsWindowSize, a.envCfg.MetricsFailureThreshold, a.metricsStore, "images")
		a.vectorsMetricsManager = metrics.NewMetricsManagerWithPersistence(
			a.envCfg.MetricsWindowSize, a.envCfg.MetricsFailureThreshold, a.metricsStore, "vectors")
	} else {
		a.messagesMetricsManager = metrics.NewMetricsManagerWithConfig(a.envCfg.MetricsWindowSize, a.envCfg.MetricsFailureThreshold)
		a.responsesMetricsManager = metrics.NewMetricsManagerWithConfig(a.envCfg.MetricsWindowSize, a.envCfg.MetricsFailureThreshold)
		a.geminiMetricsManager = metrics.NewMetricsManagerWithConfig(a.envCfg.MetricsWindowSize, a.envCfg.MetricsFailureThreshold)
		a.chatMetricsManager = metrics.NewMetricsManagerWithConfig(a.envCfg.MetricsWindowSize, a.envCfg.MetricsFailureThreshold)
		a.imagesMetricsManager = metrics.NewMetricsManagerWithConfig(a.envCfg.MetricsWindowSize, a.envCfg.MetricsFailureThreshold)
		a.vectorsMetricsManager = metrics.NewMetricsManagerWithConfig(a.envCfg.MetricsWindowSize, a.envCfg.MetricsFailureThreshold)
	}
	a.traceAffinityManager = session.NewTraceAffinityManager()

	applyCircuitBreakerConfig := func(cfg config.Config) {
		requestTimeoutMs := a.envCfg.RequestTimeout
		responseHeaderTimeoutMs := a.envCfg.ResponseHeaderTimeout * 1000
		params := metrics.CircuitBreakerParams{
			WindowSize:                   a.envCfg.MetricsWindowSize,
			FailureThreshold:             a.envCfg.MetricsFailureThreshold,
			ConsecutiveFailuresThreshold: 5,
			StreamFirstContentTimeoutMs:  90000,
			StreamInactivityTimeoutMs:    90000,
			StreamToolCallIdleTimeoutMs:  300000,
		}
		if cfg.CircuitBreaker != nil {
			if cfg.CircuitBreaker.WindowSize != nil {
				params.WindowSize = *cfg.CircuitBreaker.WindowSize
			}
			if cfg.CircuitBreaker.FailureThreshold != nil {
				params.FailureThreshold = *cfg.CircuitBreaker.FailureThreshold
			}
			if cfg.CircuitBreaker.ConsecutiveFailuresThreshold != nil {
				params.ConsecutiveFailuresThreshold = int64(*cfg.CircuitBreaker.ConsecutiveFailuresThreshold)
			}
			if cfg.CircuitBreaker.RequestTimeoutMs != nil {
				requestTimeoutMs = *cfg.CircuitBreaker.RequestTimeoutMs
			}
			if cfg.CircuitBreaker.ResponseHeaderTimeoutMs != nil {
				responseHeaderTimeoutMs = *cfg.CircuitBreaker.ResponseHeaderTimeoutMs
			}
			if cfg.CircuitBreaker.StreamFirstContentTimeoutMs != nil {
				params.StreamFirstContentTimeoutMs = *cfg.CircuitBreaker.StreamFirstContentTimeoutMs
			}
			if cfg.CircuitBreaker.StreamInactivityTimeoutMs != nil {
				params.StreamInactivityTimeoutMs = *cfg.CircuitBreaker.StreamInactivityTimeoutMs
			}
			if cfg.CircuitBreaker.StreamToolCallIdleTimeoutMs != nil {
				params.StreamToolCallIdleTimeoutMs = *cfg.CircuitBreaker.StreamToolCallIdleTimeoutMs
			}
		}
		config.SetRuntimeTimeouts(requestTimeoutMs, responseHeaderTimeoutMs)
		a.messagesMetricsManager.UpdateCircuitBreakerConfig(params)
		a.responsesMetricsManager.UpdateCircuitBreakerConfig(params)
		a.geminiMetricsManager.UpdateCircuitBreakerConfig(params)
		a.chatMetricsManager.UpdateCircuitBreakerConfig(params)
		a.imagesMetricsManager.UpdateCircuitBreakerConfig(params)
		a.vectorsMetricsManager.UpdateCircuitBreakerConfig(params)
	}
	applyCircuitBreakerConfig(a.cfgManager.GetConfig())
	a.cfgManager.RegisterOnConfigChange(applyCircuitBreakerConfig)
}
