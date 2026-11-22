package main

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/conversation"
	"github.com/soulteary/yunqi/internal/guardrails"
	"github.com/soulteary/yunqi/internal/ratelimit"
	"github.com/soulteary/yunqi/internal/redaction"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/thinkingcache"
	"github.com/soulteary/yunqi/internal/utils"
	"github.com/soulteary/yunqi/internal/warmup"
)

func (a *application) configureGuardrails() {
	// 初始化 Guardrails 注册表：credential-masker（最小集）
	// fail-open：guardrail 异常仅记日志放行，绝不阻断流量
	credentialMasker := guardrails.NewCredentialMasker()
	// 注入网关自身密钥
	gatewayKeys := []string{a.envCfg.ProxyAccessKey, a.envCfg.GetAdminAccessKey()}
	if a.envCfg.IdentityProxySecret != "" {
		gatewayKeys = append(gatewayKeys, a.envCfg.IdentityProxySecret)
	}
	gatewayKeys = append(gatewayKeys, a.envCfg.ExtraProxyAccessKeys...)
	credentialMasker.SetStaticKeys(gatewayKeys)
	initializeCredentialRedaction(a.envCfg)
	redaction.Add(gatewayKeys)
	updateCredentials := func(cfg config.Config) {
		if data, err := json.Marshal(cfg); err == nil {
			redaction.ObserveJSON(data)
		}
		var prefixes []string
		for _, kind := range config.ChannelKindRegistry {
			for _, upstream := range kind.Slice(cfg) {
				for _, key := range upstream.APIKeys {
					if len(key) >= 8 {
						prefixes = append(prefixes, key[:8])
					}
				}
			}
		}
		credentialMasker.SetChannelKeyPrefixes(prefixes)
	}
	updateCredentials(a.cfgManager.GetConfig())
	guardrails.DefaultRegistry().Register(credentialMasker)
	log.Printf("[Guardrails-Init] 已注册 credential-masker，错误与日志出口完整凭证脱敏已启用")
	a.cfgManager.RegisterOnConfigChange(updateCredentials)
}

func (a *application) configureThinkingCache() {
	applyThinkingCacheConfig := func(cfg config.Config) {
		if err := thinkingcache.Configure(thinkingcache.Config{
			DBPath: a.paths.ThinkingCacheDBPath,
			TTL:    cfg.ThinkingCache.EffectiveTTL(),
		}); err != nil {
			log.Printf("[ThinkingCache-Init] 警告: 初始化 Claude thinking 缓存失败: %v，将使用内存缓存", err)
		}
	}
	applyThinkingCacheConfig(a.cfgManager.GetConfig())
	a.cfgManager.RegisterOnConfigChange(applyThinkingCacheConfig)
}

func (a *application) configureSchedulerTuning() {
	// 调度软增强（匿名请求内容指纹亲和回退 + per-key 自动权重），热重载生效
	applySchedulerTuning := func(cfg config.Config) {
		tuning := config.SchedulerConfig{}
		if cfg.Scheduler != nil {
			tuning = *cfg.Scheduler
		}
		config.ApplySchedulerTuning(tuning)
		utils.SetPromptAffinityFallback(tuning.PromptAffinityFallbackEnabled())
	}
	applySchedulerTuning(a.cfgManager.GetConfig())
	a.cfgManager.RegisterOnConfigChange(applySchedulerTuning)
}

func (a *application) initSessionManager() {
	// 初始化会话管理器（Responses API 专用）
	a.sessionManager = session.NewSessionManager(
		24*time.Hour, // 24小时过期
		100,          // 最多100条消息
		100000,       // 最多100k tokens
	)
	log.Printf("[Session-Init] 会话管理器已初始化")
}

func (a *application) initRateLimits() {
	// 初始化主动限速管理器（渠道级令牌桶 + 并发控制 + 429 动态 cooldown）
	a.rateLimitManager = ratelimit.NewManager()
	applyRateLimitConfig := func(cfg config.Config) {
		channelTypes := []struct {
			apiType   string
			upstreams []config.UpstreamConfig
		}{
			{"Messages", cfg.Upstream},
			{"Chat", cfg.ChatUpstream},
			{"Responses", cfg.ResponsesUpstream},
			{"Gemini", cfg.GeminiUpstream},
			{"Images", cfg.ImagesUpstream},
			{"Vectors", cfg.VectorsUpstream},
		}
		entries := make([]ratelimit.ChannelConfig, 0)
		for _, ct := range channelTypes {
			for idx, upstream := range ct.upstreams {
				autoFromHeaders := upstream.RateLimitAutoFromHeaders != nil && *upstream.RateLimitAutoFromHeaders
				entries = append(entries, ratelimit.ChannelConfig{
					APIType:      ct.apiType,
					ChannelIndex: idx,
					ChannelUID:   upstream.ChannelUID,
					Config: ratelimit.Config{
						RPM:             upstream.RateLimitRPM,
						WindowSeconds:   config.RateLimitWindowSeconds(upstream.RateLimitWindowMinutes),
						MaxConcurrent:   upstream.RateLimitMaxConcurrent,
						AutoFromHeaders: autoFromHeaders,
					},
				})
			}
		}
		a.rateLimitManager.ReconcileChannelConfigs(entries)
	}
	applyRateLimitConfig(a.cfgManager.GetConfig())
	a.cfgManager.RegisterOnConfigChange(applyRateLimitConfig)
	log.Printf("[RateLimit-Init] 主动限速管理器已初始化")
}

func (a *application) initURLManager() {
	// 初始化 URL 管理器（非阻塞，动态排序）
	a.urlManager = warmup.NewURLManager(30*time.Second, 3) // 30秒冷却期，连续3次失败后移到末尾
	log.Printf("[URLManager-Init] URL管理器已初始化 (冷却期: 30秒, 最大连续失败: 3)")
}

func (a *application) initConversations() {
	// 初始化对话追踪器和覆盖管理器
	a.conversationTracker = conversation.NewConversationTracker(1*time.Hour, 24*time.Hour, a.paths.ConversationStatePath)

	// 获取 override TTL：优先使用配置文件中的值，否则使用环境变量
	cfg := a.cfgManager.GetConfig()
	overrideTTLMinutes := cfg.OverrideTTLMinutes
	if overrideTTLMinutes == 0 {
		overrideTTLMinutes = a.envCfg.OverrideTTLMinutes
	}

	var overrideTTL time.Duration
	if overrideTTLMinutes == -1 {
		overrideTTL = -1 // 永不过期
		log.Printf("[Conversation-Init] 对话追踪器和覆盖管理器已初始化 (idle: 1h, expire: 2h, override TTL: 永不恢复)")
	} else {
		overrideTTL = time.Duration(overrideTTLMinutes) * time.Minute
		log.Printf("[Conversation-Init] 对话追踪器和覆盖管理器已初始化 (idle: 1h, expire: 2h, override TTL: %dm)", overrideTTLMinutes)
	}

	a.overrideManager = conversation.NewOverrideManager(overrideTTL)
	a.channelScheduler.SetConversationComponents(a.conversationTracker, a.overrideManager)
}

func initializeCredentialRedaction(env *config.EnvConfig) func() {
	keys := []string{env.ProxyAccessKey, env.GetAdminAccessKey(), env.IdentityProxySecret, env.ProxyRedisURL}
	if parsed, err := url.Parse(env.ProxyRedisURL); err == nil && parsed.User != nil {
		if password, ok := parsed.User.Password(); ok {
			keys = append(keys, password)
		}
	}
	keys = append(keys, env.ExtraProxyAccessKeys...)
	if secret := os.Getenv("IDENTITY_PROXY_SECRET"); secret != "" {
		keys = append(keys, secret)
	}
	keys = append(keys, env.IdentityProxyRevocationRedisURL)
	if parsed, err := url.Parse(env.IdentityProxyRevocationRedisURL); err == nil && parsed.User != nil {
		if password, ok := parsed.User.Password(); ok {
			keys = append(keys, password)
		}
	}
	return redaction.Enable(keys)
}

func (a *application) configureBodyLogging() {
	apply := func() {
		if _, err := a.cfgManager.ActivateBodyLogging("system:config-reload"); err != nil {
			log.Printf("[BodyLog-Policy] 正文日志策略未启用，审计/保存失败: %v", err)
		}
	}
	if _, err := a.cfgManager.ActivateBodyLogging("system:startup"); err != nil {
		log.Printf("[BodyLog-Policy] 正文日志策略未启用，审计/保存失败: %v", err)
	}
	a.cfgManager.RegisterOnConfigChange(func(_ config.Config) { apply() })
}

func initializeIdentityRevocationSync(env *config.EnvConfig, cm *config.ConfigManager) {
	apply := func() {
		cfg := cm.IdentityProxySnapshot()
		if cfg.EffectiveRevocationMode() != "shared" || env.IdentityProxyRevocations == nil {
			return
		}
		call, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := env.IdentityProxyRevocations.Prepare(call, cfg.RevocationEpoch, false)
		if err == nil {
			err = env.IdentityProxyRevocations.Sync(call, cfg.RevokedSessions)
		}
		if err != nil {
			log.Print("[Identity-Revocation] shared revocation sync unavailable; shared authentication remains fail closed")
		}
	}
	apply()
	cm.RegisterOnConfigChange(func(config.Config) { apply() })
}
