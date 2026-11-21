package autopilot

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// AutoAddRequest POST /:kind/channels/auto-add 请求体。
//
// 两种模式：
//   - provider 模板模式：带 ProviderID + APIKeys，系统自动判别 baseURL（按 key 前缀探测验证），无需填 BaseURLs
//   - 自定义模式：带 BaseURLs + APIKeys；可通过 Routes 原子创建探测成功的多协议路由
type AutoAddRequest struct {
	Name            string                `json:"name,omitempty"`
	ProviderID      string                `json:"providerId,omitempty"`
	BaseURLs        []string              `json:"baseUrls"`
	APIKeys         []string              `json:"apiKeys"`
	Routes          []AutoAddRouteRequest `json:"routes,omitempty"`
	RateLimitHint   *AutoAddRateLimitHint `json:"rateLimitHint,omitempty"`
	SubscriptionUID string                `json:"subscriptionUid,omitempty"`
	// ProxyURL 渠道级代理（HTTP/HTTPS/SOCKS5）：发现/探活与后续上游请求经代理访问，
	// 用于添加直连不可达的境外上游。
	ProxyURL string `json:"proxyUrl,omitempty"`
	// ProxyPreferDirect 直连优先：配置了代理时先直连，失败自动回退代理。
	ProxyPreferDirect bool `json:"proxyPreferDirect,omitempty"`
	// Placement 故障转移位置："front"（首位）| 缺省末尾
	Placement string `json:"placement,omitempty"`
}

// AutoAddRateLimitHint 是添加前主动探测得到的 endpoint 限速提示，不是用户显式限额。
type AutoAddRateLimitHint struct {
	InitialRPM       int  `json:"initialRpm"`
	EffectiveRPM     int  `json:"effectiveRpm"`
	RateLimited      bool `json:"rateLimited"`
	RateLimitedCount int  `json:"rateLimitedCount,omitempty"`
}

// AutoAddRouteRequest 描述自定义渠道探测成功的一条原生协议路由。
type AutoAddRouteRequest struct {
	ChannelKind     string   `json:"channelKind"`
	SupportedModels []string `json:"supportedModels,omitempty"`
}

// AutoAddResponse POST /:kind/channels/auto-add 响应体。
type AutoAddResponse struct {
	AccountUID       string                 `json:"accountUid"`
	ChannelUID       string                 `json:"channelUid"`
	Index            int                    `json:"index"`
	DiscoveryStarted bool                   `json:"discoveryStarted"`
	Channels         []AutoAddChannelResult `json:"channels,omitempty"`
}

// AutoAddChannelResult 描述 provider 快速添加一次创建出的单条渠道。
type AutoAddChannelResult struct {
	AccountUID       string `json:"accountUid"`
	ChannelKind      string `json:"channelKind"`
	ChannelUID       string `json:"channelUid"`
	Index            int    `json:"index"`
	Name             string `json:"name"`
	ServiceType      string `json:"serviceType"`
	DiscoveryStarted bool   `json:"discoveryStarted"`
}

// AutoStatusResponse GET /:kind/channels/:id/auto-status 响应体。
type AutoStatusResponse struct {
	AutoManaged   bool                 `json:"autoManaged"`
	AutoManagedAt *time.Time           `json:"autoManagedAt,omitempty"`
	Discovery     *DiscoveryStatusInfo `json:"discovery,omitempty"`
}

// DiscoveryStatusInfo 发现状态信息。
type DiscoveryStatusInfo struct {
	Status     DiscoveryStatus         `json:"status"`
	StartedAt  *time.Time              `json:"startedAt,omitempty"`
	FinishedAt *time.Time              `json:"finishedAt,omitempty"`
	Error      string                  `json:"error,omitempty"`
	Endpoints  []EndpointDiscoveryInfo `json:"endpoints"`
}

// EndpointDiscoveryInfo 端点发现结果（key 已掩码）。
type EndpointDiscoveryInfo struct {
	KeyMask                  string               `json:"keyMask"`
	BaseURL                  string               `json:"baseUrl"`
	ModelsCount              int                  `json:"modelsCount"`
	ProtocolOk               bool                 `json:"protocolOk"`
	ModelDiscoverySource     string               `json:"modelDiscoverySource,omitempty"`
	ModelDiscoveryMessage    string               `json:"modelDiscoveryMessage,omitempty"`
	ModelsDiscoveredAt       *time.Time           `json:"modelsDiscoveredAt,omitempty"`
	ProtocolModels           map[string][]string  `json:"protocolModels,omitempty"`
	ProtocolDiscoveredAt     map[string]time.Time `json:"protocolDiscoveredAt,omitempty"`
	ProtocolDiscoverySource  map[string]string    `json:"protocolDiscoverySource,omitempty"`
	ProtocolDiscoveryMessage map[string]string    `json:"protocolDiscoveryMessage,omitempty"`
	ProtocolDiscoveryError   map[string]string    `json:"protocolDiscoveryError,omitempty"`
}

// AutoManagedDeps 自动托管路由的依赖注入。
type AutoManagedDeps struct {
	CfgManager             *config.ConfigManager
	Runner                 *AutoDiscoveryRunner
	RateLimitDiscoverer    *RateLimitDiscoverer
	ResetChannelMetrics    func(kind string, index int)
	VerifyChannelKey       func(ctx context.Context, kind string, upstream config.UpstreamConfig, apiKey string) error
	SkipChannelKeyVerify   bool
	MiMoConsoleClient      *MiMoConsoleClient
	CompshareConsoleClient *CompshareConsoleClient
	KimiConsoleClient      *KimiConsoleClient
	DeepSeekClient         *DeepSeekClient
}

// RegisterAutoManagedRoutes 注册自动托管 API 路由。
// 路由直接挂载到 apiGroup（不创建子组），与现有渠道管理路由共存。
//
// 注意：必须为每个 kind 显式注册静态路径，不能用 `:kind` 参数，
// 否则会与现有的 `/messages/channels/...` 等静态路由在 Fiber radix tree 中冲突。
func RegisterAutoManagedRoutes(apiGroup fiber.Router, deps *AutoManagedDeps) {
	apiGroup.Get("/accounts", handleListAccounts(deps))
	apiGroup.Patch("/accounts/:accountUid", handleRenameAccount(deps))
	apiGroup.Put("/accounts/:accountUid", handleUpdateAccount(deps))
	apiGroup.Delete("/accounts/:accountUid", handleDeleteAccount(deps))
	apiGroup.Post("/accounts/:accountUid/credentials", handleAddAccountCredentials(deps))
	apiGroup.Patch("/accounts/:accountUid/credentials", handlePatchAccountCredentials(deps))
	apiGroup.Delete("/accounts/:accountUid/credentials/:credentialUid", handleDeleteAccountCredential(deps))
	apiGroup.Put("/accounts/:accountUid/credentials/:credentialUid/volcengine-access-key", handleSetVolcengineAccessKey(deps))
	apiGroup.Delete("/accounts/:accountUid/credentials/:credentialUid/volcengine-access-key", handleClearVolcengineAccessKey(deps))
	apiGroup.Post("/accounts/:accountUid/credentials/:credentialUid/volcengine-plan-usage/refresh", handleRefreshVolcenginePlanUsage(deps))
	apiGroup.Put("/accounts/:accountUid/credentials/:credentialUid/mimo-console-cookie", handleSetMiMoConsoleCookie(deps))
	apiGroup.Post("/accounts/:accountUid/credentials/:credentialUid/mimo-console-cookie/refresh", handleRefreshMiMoConsoleCookie(deps))
	apiGroup.Delete("/accounts/:accountUid/credentials/:credentialUid/mimo-console-cookie", handleClearMiMoConsoleCookie(deps))
	apiGroup.Put("/accounts/:accountUid/credentials/:credentialUid/compshare-console-cookie", handleSetCompshareConsoleCookie(deps))
	apiGroup.Post("/accounts/:accountUid/credentials/:credentialUid/compshare-console-cookie/refresh", handleRefreshCompshareConsoleCookie(deps))
	apiGroup.Delete("/accounts/:accountUid/credentials/:credentialUid/compshare-console-cookie", handleClearCompshareConsoleCookie(deps))
	apiGroup.Put("/accounts/:accountUid/credentials/:credentialUid/kimi-console-token", handleSetKimiConsoleToken(deps))
	apiGroup.Post("/accounts/:accountUid/credentials/:credentialUid/kimi-console-token/refresh", handleRefreshKimiConsoleToken(deps))
	apiGroup.Delete("/accounts/:accountUid/credentials/:credentialUid/kimi-console-token", handleClearKimiConsoleToken(deps))
	apiGroup.Get("/accounts/:accountUid/deepseek-balance", handleDeepSeekBalance(deps))
	kinds := []string{"messages", "chat", "responses", "gemini", "images", "vectors"}
	for _, kind := range kinds {
		apiGroup.Post("/"+kind+"/channels/auto-add", handleAutoAdd(deps))
		apiGroup.Post("/"+kind+"/channels/:id/auto-discover", handleAutoDiscover(deps))
		apiGroup.Get("/"+kind+"/channels/:id/auto-status", handleAutoStatus(deps))
	}
}

// handleAutoAdd POST /{kind}/channels/auto-add
// 创建自动托管渠道并异步触发发现。
func handleAutoAdd(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		kind := extractKindFromPath(c)
		if !validChannelKinds[kind] {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": fmt.Sprintf("不支持的渠道类型: %s", kind)})
			return nil
		}

		var req AutoAddRequest
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "无效的请求体"})
			return nil
		}
		req.ProviderID = strings.ToLower(strings.TrimSpace(req.ProviderID))
		req.BaseURLs = uniqueNonEmptyStrings(req.BaseURLs)
		req.APIKeys = uniqueNonEmptyStrings(req.APIKeys)
		if len(req.APIKeys) == 0 {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "apiKeys 不能为空"})
			return nil
		}
		if req.ProviderID == "" {
			req.ProviderID = inferAutoAddProviderID(req.BaseURLs, req.APIKeys)
		}
		// provider 模板模式无需 baseUrls（由模板判定）；自定义模式必须带 baseUrls
		if req.ProviderID == "" && len(req.BaseURLs) == 0 {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "baseUrls 不能为空"})
			return nil
		}

		if req.ProviderID != "" {
			handleProviderAutoAdd(c, deps, kind, req)
			return nil
		}

		handleCustomAutoAdd(c, deps, kind, req)
		return nil
	}
}
