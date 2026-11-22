package autopilot

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/utils"
)

// inferAutoAddProviderID 只在输入没有歧义时提升为已知 provider 模式。
// 明确填写非模板 URL 时绝不根据 Key 样式覆盖用户选择。
func inferAutoAddProviderID(baseURLs, apiKeys []string) string {
	baseURLs = uniqueNonEmptyStrings(baseURLs)
	if len(baseURLs) > 0 {
		providerID := ""
		for _, baseURL := range baseURLs {
			inferred, ok := config.InferProviderIDFromBaseURL(baseURL)
			if !ok || (providerID != "" && providerID != inferred) {
				return ""
			}
			providerID = inferred
		}
		return providerID
	}

	providerID := ""
	for _, apiKey := range uniqueNonEmptyStrings(apiKeys) {
		inferred, ok := config.InferProviderIDFromAPIKey(apiKey)
		if !ok || (providerID != "" && providerID != inferred) {
			return ""
		}
		providerID = inferred
	}
	return providerID
}

func handleProviderAutoAdd(c fiber.Ctx, deps *AutoManagedDeps, requestKind string, req AutoAddRequest) {
	managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

	tmpl, ok := config.GetProviderTemplate(req.ProviderID)
	if !ok {
		httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": fmt.Sprintf("未知的 provider: %s", req.ProviderID)})
		return
	}
	routes := tmpl.AutoAddRoutes()
	if len(routes) == 0 {
		httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": fmt.Sprintf("provider %s 未配置可添加的渠道 route", req.ProviderID)})
		return
	}
	if !tmpl.SupportsChannelKind(requestKind) {
		httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": fmt.Sprintf("provider %s 不支持添加到 %s 渠道", req.ProviderID, requestKind)})
		return
	}
	if accountUID := managedAccountUIDForProvider(deps.CfgManager.GetConfig(), tmpl.ProviderID); accountUID != "" {
		appendCredentialsToProviderAccount(c, deps, requestKind, accountUID, req.APIKeys)
		return
	}

	// provider 账号是按 providerId 唯一的托管资源，不接受客户端自定义名称。
	// 使用 providerId 作为保留名（如 mimo），前端展示时再映射为品牌友好名。
	baseName := tmpl.ProviderID

	cfg := deps.CfgManager.GetConfig()
	for _, route := range routes {
		name := providerRouteName(baseName, route, len(routes) > 1)
		if channelNameExists(getChannelSlice(cfg, route.ChannelKind), name) {
			httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": fmt.Sprintf("渠道名称 '%s' 已存在", name)})
			return
		}
	}

	type plannedRoute struct {
		route      config.ProviderRoute
		name       string
		keyConfigs []config.APIKeyConfig
		baseURLs   []string
	}
	planned := make([]plannedRoute, 0, len(routes))
	for _, route := range routes {
		keyConfigs, baseURLs, verr := verifyProviderRouteKeys(httpbridge.Request(c).Context(), tmpl, route, req.APIKeys)
		if verr != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": verr.Error()})
			return
		}
		planned = append(planned, plannedRoute{
			route:      route,
			name:       providerRouteName(baseName, route, len(routes) > 1),
			keyConfigs: keyConfigs,
			baseURLs:   baseURLs,
		})
	}

	now := time.Now()
	accountUID := config.GenerateAccountUID()
	additions := make([]config.AccountChannelAddition, 0, len(planned))
	for _, item := range planned {
		for i := range item.keyConfigs {
			item.keyConfigs[i].CredentialUID = config.GenerateCredentialUID(accountUID, item.keyConfigs[i].Key)
		}
		upstream := config.UpstreamConfig{
			Name:            item.name,
			AccountUID:      accountUID,
			ChannelUID:      config.GenerateChannelUID(),
			ServiceType:     item.route.ServiceType,
			Status:          "active",
			AutoManaged:     true,
			AutoManagedAt:   &now,
			ProviderID:      tmpl.ProviderID,
			OriginType:      tmpl.OriginType,
			OriginTier:      tmpl.OriginTier,
			BaseURL:         item.baseURLs[0],
			BaseURLs:        item.baseURLs,
			APIKeys:         append([]string(nil), req.APIKeys...),
			APIKeyConfigs:   item.keyConfigs,
			SupportedModels: append([]string(nil), item.route.SupportedModels...),
		}
		config.ApplyProviderUpstreamDefaults(tmpl.ProviderID, &upstream)
		additions = append(additions, config.AccountChannelAddition{Kind: item.route.ChannelKind, Upstream: upstream, Placement: req.Placement})
	}
	if err := deps.CfgManager.ApplyAccountChannelChangesAudited(managementAuditContext, accountUID, nil, additions); err != nil {
		httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": fmt.Sprintf("创建渠道失败: %v", err)})
		return
	}

	cfg = deps.CfgManager.GetConfig()
	results := make([]AutoAddChannelResult, 0, len(additions))
	for _, addition := range additions {
		upstream := addition.Upstream
		index := findChannelIndexByUID(getChannelSlice(cfg, addition.Kind), upstream.ChannelUID)
		discoveryStarted := triggerDiscoveryForChannel(deps, addition.Kind, upstream.ChannelUID)
		results = append(results, AutoAddChannelResult{
			AccountUID:       accountUID,
			ChannelKind:      addition.Kind,
			ChannelUID:       upstream.ChannelUID,
			Index:            index,
			Name:             upstream.Name,
			ServiceType:      upstream.ServiceType,
			DiscoveryStarted: discoveryStarted,
		})
		log.Printf("[AutoManaged-Add] 创建自动托管 provider 渠道: provider=%s kind=%s serviceType=%s name=%s uid=%s",
			tmpl.ProviderID, addition.Kind, upstream.ServiceType, upstream.Name, upstream.ChannelUID)
	}

	primary := primaryAutoAddResult(results, requestKind)
	httpbridge.JSON(c, http.StatusCreated, AutoAddResponse{
		AccountUID:       accountUID,
		ChannelUID:       primary.ChannelUID,
		Index:            primary.Index,
		DiscoveryStarted: primary.DiscoveryStarted,
		Channels:         results,
	})
}

func managedAccountUIDForProvider(cfg config.Config, providerID string) string {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return ""
	}
	uid := ""
	for _, account := range cfg.ManagedAccounts {
		if account.ProviderID == providerID {
			uid = account.AccountUID
		}
	}
	return uid
}

func appendCredentialsToProviderAccount(c fiber.Ctx, deps *AutoManagedDeps, requestKind, accountUID string, apiKeys []string) {
	managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

	channels := deps.CfgManager.GetAccountChannels(accountUID)
	if len(channels) == 0 {
		httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "provider 账号不存在"})
		return
	}
	var desired []string
	for _, channel := range channels {
		desired = append(desired, channel.Upstream.APIKeys...)
	}
	desired = uniqueNonEmptyStrings(append(desired, apiKeys...))
	tmpl, ok := config.GetProviderTemplate(channels[0].Upstream.ProviderID)
	if !ok {
		httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "provider 模板不存在"})
		return
	}
	additions, status, err := planProviderAccountRouteAdditions(httpbridge.Request(c).Context(), deps.CfgManager.GetConfig(), tmpl, accountUID, desired, channels)
	if err != nil {
		httpbridge.JSON(c, status, fiber.Map{"error": err.Error()})
		return
	}
	accountName := strings.TrimSuffix(channels[0].Upstream.Name, accountRouteSuffix(channels[0].Kind))
	updates, status, err := planManagedAccountUpdates(httpbridge.Request(c).Context(), accountUID, updateAccountRequest{
		Name: accountName, APIKeys: desired,
	}, channels, tmpl, len(channels)+len(additions))
	if err != nil {
		httpbridge.JSON(c, status, fiber.Map{"error": err.Error()})
		return
	}
	changed := channelsWithAddedKeys(channels, desired)
	if err := deps.CfgManager.ApplyAccountChannelChangesAudited(managementAuditContext, accountUID, updates, additions); err != nil {
		httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": err.Error()})
		return
	}
	resetChangedChannelMetrics(deps, changed)
	for _, addition := range additions {
		log.Printf("[AutoManaged-Add] 已补齐 provider 账号渠道: provider=%s account=%s kind=%s serviceType=%s uid=%s",
			tmpl.ProviderID, accountUID, addition.Kind, addition.Upstream.ServiceType, addition.Upstream.ChannelUID)
	}

	channels = deps.CfgManager.GetAccountChannels(accountUID)
	discoveryStarted := make(map[string]bool, len(channels))
	for _, channel := range channels {
		discoveryStarted[channel.Upstream.ChannelUID] = triggerDiscoveryForChannel(deps, channel.Kind, channel.Upstream.ChannelUID)
	}
	results := make([]AutoAddChannelResult, 0, len(channels))
	cfg := deps.CfgManager.GetConfig()
	for _, channel := range channels {
		index := findChannelIndexByUID(getChannelSlice(cfg, channel.Kind), channel.Upstream.ChannelUID)
		results = append(results, AutoAddChannelResult{
			AccountUID: accountUID, ChannelKind: channel.Kind, ChannelUID: channel.Upstream.ChannelUID,
			Index: index, Name: channel.Upstream.Name, ServiceType: channel.Upstream.ServiceType,
			DiscoveryStarted: discoveryStarted[channel.Upstream.ChannelUID],
		})
	}
	primary := primaryAutoAddResult(results, requestKind)
	log.Printf("[AutoManaged-Add] 已向 provider 账号追加凭证: provider=%s account=%s keys=%d", channels[0].Upstream.ProviderID, accountUID, len(apiKeys))
	httpbridge.JSON(c, http.StatusOK, AutoAddResponse{
		AccountUID: accountUID, ChannelUID: primary.ChannelUID, Index: primary.Index,
		DiscoveryStarted: primary.DiscoveryStarted, Channels: results,
	})
}

func planProviderAccountRouteAdditions(
	ctx context.Context,
	cfg config.Config,
	tmpl *config.ProviderTemplate,
	accountUID string,
	apiKeys []string,
	existing []config.AccountChannel,
) ([]config.AccountChannelAddition, int, error) {
	if tmpl == nil || len(existing) == 0 {
		return nil, http.StatusNotFound, fmt.Errorf("provider 账号不存在")
	}
	missing := missingProviderAccountRoutes(tmpl, existing)
	if len(missing) == 0 {
		return nil, http.StatusOK, nil
	}

	baseName := strings.TrimSuffix(existing[0].Upstream.Name, accountRouteSuffix(existing[0].Kind))
	allRoutes := tmpl.AutoAddRoutes()
	for _, route := range missing {
		name := providerRouteName(baseName, route, len(allRoutes) > 1)
		if channelNameExists(getChannelSlice(cfg, route.ChannelKind), name) {
			return nil, http.StatusConflict, fmt.Errorf("渠道名称 '%s' 已存在", name)
		}
	}

	existingKeys := make(map[string]bool)
	for _, channel := range existing {
		for _, apiKey := range channel.Upstream.APIKeys {
			existingKeys[apiKey] = true
		}
	}
	var addedKeys []string
	for _, apiKey := range uniqueNonEmptyStrings(apiKeys) {
		if !existingKeys[apiKey] {
			addedKeys = append(addedKeys, apiKey)
		}
	}

	now := time.Now()
	additions := make([]config.AccountChannelAddition, 0, len(missing))
	affinities := providerKeyCandidateAffinities(tmpl, existing)
	for _, route := range missing {
		keyConfigs, _, err := bindProviderRouteKeysWithAffinities(tmpl, route, apiKeys, affinities)
		if err != nil {
			return nil, http.StatusBadRequest, err
		}
		if len(addedKeys) > 0 {
			verified, _, err := verifyProviderRouteKeys(ctx, tmpl, route, addedKeys)
			if err != nil {
				return nil, http.StatusBadRequest, err
			}
			verifiedByKey := make(map[string]config.APIKeyConfig, len(verified))
			for _, keyConfig := range verified {
				verifiedByKey[keyConfig.Key] = keyConfig
			}
			for i := range keyConfigs {
				if verifiedConfig, ok := verifiedByKey[keyConfigs[i].Key]; ok {
					keyConfigs[i] = verifiedConfig
				}
			}
		}
		baseURLs := make([]string, 0, len(keyConfigs))
		for i := range keyConfigs {
			keyConfigs[i].CredentialUID = config.GenerateCredentialUID(accountUID, keyConfigs[i].Key)
			baseURLs = append(baseURLs, keyConfigs[i].BaseURL)
		}
		upstream := config.UpstreamConfig{
			Name:            providerRouteName(baseName, route, len(allRoutes) > 1),
			AccountUID:      accountUID,
			ChannelUID:      config.GenerateChannelUID(),
			ServiceType:     route.ServiceType,
			Status:          "active",
			AutoManaged:     true,
			AutoManagedAt:   &now,
			ProviderID:      tmpl.ProviderID,
			OriginType:      tmpl.OriginType,
			OriginTier:      tmpl.OriginTier,
			BaseURL:         baseURLs[0],
			BaseURLs:        uniqueNonEmptyStrings(baseURLs),
			APIKeys:         append([]string(nil), apiKeys...),
			APIKeyConfigs:   keyConfigs,
			SupportedModels: append([]string(nil), route.SupportedModels...),
		}
		config.ApplyProviderUpstreamDefaults(tmpl.ProviderID, &upstream)
		additions = append(additions, config.AccountChannelAddition{Kind: route.ChannelKind, Upstream: upstream})
	}
	return additions, http.StatusOK, nil
}

// bindProviderRouteKeys 为已验证过的托管账号凭证选择 route 的首选候选端点。
// 旧账号补齐新协议时不应重新发送模型请求；端点可达性由随后触发的 discovery 异步确认。
func bindProviderRouteKeys(tmpl *config.ProviderTemplate, route config.ProviderRoute, apiKeys []string) ([]config.APIKeyConfig, []string, error) {
	return bindProviderRouteKeysWithAffinities(tmpl, route, apiKeys, nil)
}

type providerCandidateAffinity struct {
	PlanTag  string
	Region   string
	Priority int
}

func bindProviderRouteKeysWithAffinities(
	tmpl *config.ProviderTemplate,
	route config.ProviderRoute,
	apiKeys []string,
	affinities map[string]providerCandidateAffinity,
) ([]config.APIKeyConfig, []string, error) {
	if tmpl == nil {
		return nil, nil, fmt.Errorf("provider 模板为空")
	}
	apiKeys = uniqueNonEmptyStrings(apiKeys)
	if len(apiKeys) == 0 {
		return nil, nil, fmt.Errorf("apiKeys 不能为空")
	}

	keyConfigs := make([]config.APIKeyConfig, 0, len(apiKeys))
	baseURLs := make([]string, 0, len(route.Candidates))
	seenBaseURL := make(map[string]bool)
	for _, apiKey := range apiKeys {
		candidates := tmpl.CandidatesForRouteKey(route, apiKey)
		if len(candidates) == 0 || strings.TrimSpace(candidates[0].BaseURL) == "" {
			return nil, nil, fmt.Errorf("provider %s 无可用候选端点（kind=%s serviceType=%s）", tmpl.ProviderID, route.ChannelKind, route.ServiceType)
		}
		selected := candidates[0]
		if affinity, ok := affinities[apiKey]; ok {
			for _, candidate := range candidates {
				if candidate.PlanTag == affinity.PlanTag && candidate.Region == affinity.Region && candidate.Priority == affinity.Priority {
					selected = candidate
					break
				}
			}
		}
		baseURL := strings.TrimSpace(selected.BaseURL)
		keyConfigs = append(keyConfigs, config.APIKeyConfig{Key: apiKey, BaseURL: baseURL})
		if !seenBaseURL[baseURL] {
			seenBaseURL[baseURL] = true
			baseURLs = append(baseURLs, baseURL)
		}
	}
	return keyConfigs, baseURLs, nil
}

func providerKeyCandidateAffinities(tmpl *config.ProviderTemplate, existing []config.AccountChannel) map[string]providerCandidateAffinity {
	affinities := make(map[string]providerCandidateAffinity)
	if tmpl == nil {
		return affinities
	}
	for _, accountChannel := range existing {
		route, ok := providerRouteForChannel(tmpl, accountChannel.Kind, accountChannel.Upstream.ServiceType)
		if !ok {
			continue
		}
		for _, apiKey := range accountChannel.Upstream.APIKeys {
			if _, exists := affinities[apiKey]; exists {
				continue
			}
			boundURL := utils.CanonicalBaseURL(accountChannel.Upstream.BoundBaseURLForKey(apiKey), route.ServiceType)
			if boundURL == "" {
				continue
			}
			for _, candidate := range tmpl.CandidatesForRouteKey(route, apiKey) {
				if utils.CanonicalBaseURL(candidate.BaseURL, route.ServiceType) == boundURL {
					affinities[apiKey] = providerCandidateAffinity{
						PlanTag: candidate.PlanTag, Region: candidate.Region, Priority: candidate.Priority,
					}
					break
				}
			}
		}
	}
	return affinities
}

func missingProviderAccountRoutes(tmpl *config.ProviderTemplate, existing []config.AccountChannel) []config.ProviderRoute {
	present := make(map[string]bool, len(existing))
	for _, channel := range existing {
		present[channel.Kind+"\x00"+channel.Upstream.ServiceType] = true
	}
	var missing []config.ProviderRoute
	for _, route := range tmpl.AutoAddRoutes() {
		if !present[route.ChannelKind+"\x00"+route.ServiceType] {
			missing = append(missing, route)
		}
	}
	return missing
}

func providerRouteName(baseName string, route config.ProviderRoute, multiRoute bool) string {
	if !multiRoute {
		return baseName
	}
	switch route.ChannelKind {
	case "messages":
		return baseName + "-claude"
	case "chat":
		return baseName + "-chat"
	case "responses":
		return baseName + "-codex"
	case "gemini":
		return baseName + "-gemini"
	default:
		return baseName + "-" + route.ChannelKind
	}
}
