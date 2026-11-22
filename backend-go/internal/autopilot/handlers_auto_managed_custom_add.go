package autopilot

import (
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

func handleCustomAutoAdd(c fiber.Ctx, deps *AutoManagedDeps, requestKind string, req AutoAddRequest) {
	managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

	routes, err := normalizeCustomAutoAddRoutes(requestKind, req.Routes)
	if err != nil {
		httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
		return
	}
	baseURLs := uniqueNonEmptyStrings(req.BaseURLs)
	if err := config.ValidateOutboundNetwork(req.OutboundNetwork, req.ProxyURL); err != nil {
		httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
		return
	}

	if len(baseURLs) == 0 {
		httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "baseUrls 不能为空"})
		return
	}
	baseName := strings.TrimSpace(req.Name)
	if baseName == "" {
		baseName = fmt.Sprintf("auto-%s-%d", requestKind, time.Now().UnixMilli()%100000)
	}

	cfg := deps.CfgManager.GetConfig()
	if duplicates := findExistingAutoAddChannels(cfg, baseURLs); len(duplicates) > 0 {
		handleExistingCustomAutoAdd(c, deps, requestKind, req, routes, duplicates)
		return
	}
	multiRoute := len(routes) > 1
	for _, route := range routes {
		name := customAutoAddRouteName(baseName, route.ChannelKind, multiRoute)
		if channelNameExists(getChannelSlice(cfg, route.ChannelKind), name) {
			httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": fmt.Sprintf("渠道名称 '%s' 已存在", name)})
			return
		}
	}

	now := time.Now()
	accountUID := config.GenerateAccountUID()
	additions := make([]config.AccountChannelAddition, 0, len(routes))
	for _, route := range routes {
		upstream := buildCustomManagedProtocolRoute(config.UpstreamConfig{
			BaseURL:           baseURLs[0],
			BaseURLs:          append([]string(nil), baseURLs...),
			APIKeys:           append([]string(nil), req.APIKeys...),
			SupportedModels:   append([]string(nil), route.SupportedModels...),
			ProxyURL:          strings.TrimSpace(req.ProxyURL),
			ProxyPreferDirect: req.ProxyPreferDirect,
			OutboundNetwork:   req.OutboundNetwork,
		}, accountUID, baseName, route.ChannelKind, multiRoute, now)
		additions = append(additions, config.AccountChannelAddition{Kind: route.ChannelKind, Upstream: upstream, Placement: req.Placement})
	}
	if err := deps.CfgManager.ApplyAccountChannelChangesAudited(managementAuditContext, accountUID, nil, additions); err != nil {
		httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": fmt.Sprintf("创建渠道失败: %v", err)})
		return
	}

	cfg = deps.CfgManager.GetConfig()
	seedAutoAddRateLimitHint(deps, deps.CfgManager.GetAccountChannels(accountUID), req.RateLimitHint, req.APIKeys[0], baseURLs[0])
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
		log.Printf("[AutoManaged-Add] 创建自定义自动托管路由: kind=%s serviceType=%s models=%d name=%s uid=%s",
			addition.Kind, upstream.ServiceType, len(upstream.SupportedModels), upstream.Name, upstream.ChannelUID)
	}
	primary := primaryAutoAddResult(results, requestKind)
	httpbridge.JSON(c, http.StatusCreated, AutoAddResponse{
		AccountUID: accountUID, ChannelUID: primary.ChannelUID, Index: primary.Index,
		DiscoveryStarted: primary.DiscoveryStarted, Channels: results,
	})
}

func handleExistingCustomAutoAdd(
	c fiber.Ctx,
	deps *AutoManagedDeps,
	requestKind string,
	req AutoAddRequest,
	routes []AutoAddRouteRequest,
	matches []existingAutoAddChannel,
) {
	for _, match := range matches {
		if match.Upstream.AccountUID != "" && match.Upstream.AutoManaged && match.Upstream.ProviderID == "" {
			appendCredentialsToCustomAccount(c, deps, requestKind, req, routes, match.Upstream.AccountUID)
			return
		}
	}
	appendCredentialsToLegacyChannels(c, deps, requestKind, req, matches)
}

func appendCredentialsToCustomAccount(
	c fiber.Ctx,
	deps *AutoManagedDeps,
	requestKind string,
	req AutoAddRequest,
	routes []AutoAddRouteRequest,
	accountUID string,
) {
	managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

	channels := deps.CfgManager.GetAccountChannels(accountUID)
	if len(channels) == 0 {
		httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "已有自动托管账号不存在"})
		return
	}

	var desiredKeys []string
	presentKinds := make(map[string]bool, len(channels))
	for _, channel := range channels {
		desiredKeys = append(desiredKeys, channel.Upstream.APIKeys...)
		presentKinds[channel.Kind] = true
	}
	desiredKeys = append(desiredKeys, req.APIKeys...)
	desiredKeys = uniqueNonEmptyStrings(desiredKeys)
	totalRouteCount := len(presentKinds)
	for _, route := range routes {
		if !presentKinds[route.ChannelKind] {
			totalRouteCount++
		}
	}

	baseName := strings.TrimSuffix(channels[0].Upstream.Name, accountRouteSuffix(channels[0].Kind))
	updates, status, err := planCustomManagedAccountUpdates(accountUID, updateAccountRequest{
		Name: baseName, APIKeys: desiredKeys,
	}, channels, totalRouteCount)
	if err != nil {
		httpbridge.JSON(c, status, fiber.Map{"error": err.Error()})
		return
	}

	baseURLs := uniqueNonEmptyStrings(req.BaseURLs)
	now := time.Now()
	additions := make([]config.AccountChannelAddition, 0, totalRouteCount-len(presentKinds))
	for _, route := range routes {
		if presentKinds[route.ChannelKind] {
			continue
		}
		name := customAutoAddRouteName(baseName, route.ChannelKind, totalRouteCount > 1)
		if channelNameExists(getChannelSlice(deps.CfgManager.GetConfig(), route.ChannelKind), name) {
			httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": fmt.Sprintf("渠道名称 '%s' 已存在", name)})
			return
		}
		upstream := buildCustomManagedProtocolRoute(config.UpstreamConfig{
			ServiceType:     channels[0].Upstream.ServiceType,
			BaseURL:         baseURLs[0],
			BaseURLs:        append([]string(nil), baseURLs...),
			APIKeys:         append([]string(nil), desiredKeys...),
			APIKeyConfigs:   customAutoAddKeyConfigs(accountUID, desiredKeys, baseURLs[0]),
			SupportedModels: append([]string(nil), route.SupportedModels...),
		}, accountUID, baseName, route.ChannelKind, totalRouteCount > 1, now)
		additions = append(additions, config.AccountChannelAddition{Kind: route.ChannelKind, Upstream: upstream, Placement: req.Placement})
	}

	if _, err := verifyAddedKeysForUpdates(httpbridge.Request(c).Context(), deps, channels, updates); err != nil {
		respondAccountUpdateError(c, http.StatusBadRequest, err)
		return
	}
	changed := channelsWithAddedKeys(channels, desiredKeys)
	if err := deps.CfgManager.ApplyAccountChannelChangesAudited(managementAuditContext, accountUID, updates, additions); err != nil {
		httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": fmt.Sprintf("追加渠道密钥失败: %v", err)})
		return
	}
	resetChangedChannelMetrics(deps, changed)
	seedAutoAddRateLimitHint(deps, deps.CfgManager.GetAccountChannels(accountUID), req.RateLimitHint, req.APIKeys[0], baseURLs[0])
	log.Printf("[AutoManaged-Add] 已向自定义账号追加凭证: account=%s keys=%d routesAdded=%d", accountUID, len(req.APIKeys), len(additions))
	respondAutoAddAccountChannels(c, deps, requestKind, accountUID)
}

func customAutoAddKeyConfigs(accountUID string, apiKeys []string, baseURL string) []config.APIKeyConfig {
	configs := make([]config.APIKeyConfig, 0, len(apiKeys))
	for _, apiKey := range apiKeys {
		configs = append(configs, config.APIKeyConfig{
			Key: apiKey, BaseURL: baseURL, CredentialUID: config.GenerateCredentialUID(accountUID, apiKey),
		})
	}
	return configs
}

func appendCredentialsToLegacyChannels(
	c fiber.Ctx,
	deps *AutoManagedDeps,
	requestKind string,
	req AutoAddRequest,
	matches []existingAutoAddChannel,
) {
	desiredKeys := uniqueNonEmptyStrings(req.APIKeys)
	channels := make([]config.AccountChannel, 0, len(matches))
	for _, match := range matches {
		channels = append(channels, config.AccountChannel{Kind: match.Kind, Upstream: match.Upstream})
	}
	if _, err := verifyNewKeysForChannels(httpbridge.Request(c).Context(), deps, channels, desiredKeys); err != nil {
		respondAccountUpdateError(c, http.StatusBadRequest, err)
		return
	}

	for _, match := range matches {
		existingKeys := make(map[string]bool, len(match.Upstream.APIKeys))
		for _, apiKey := range match.Upstream.APIKeys {
			existingKeys[apiKey] = true
		}
		for _, apiKey := range desiredKeys {
			if existingKeys[apiKey] {
				continue
			}
			if err := appendAPIKeyToExistingChannel(deps.CfgManager, match.Kind, match.Index, apiKey); err != nil {
				httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": fmt.Sprintf("向已有渠道追加密钥失败: %v", err)})
				return
			}
			existingKeys[apiKey] = true
			resetChangedChannelMetrics(deps, []existingChannelRef{{Kind: match.Kind, ChannelUID: match.Upstream.ChannelUID}})
		}
	}

	cfg := deps.CfgManager.GetConfig()
	matchedChannels := make([]config.AccountChannel, 0, len(matches))
	for _, match := range matches {
		index := findChannelIndexByUID(getChannelSlice(cfg, match.Kind), match.Upstream.ChannelUID)
		if index >= 0 {
			matchedChannels = append(matchedChannels, config.AccountChannel{Kind: match.Kind, Upstream: getChannelSlice(cfg, match.Kind)[index]})
		}
	}
	seedAutoAddRateLimitHint(deps, matchedChannels, req.RateLimitHint, req.APIKeys[0], req.BaseURLs[0])
	results := make([]AutoAddChannelResult, 0, len(matches))
	for _, match := range matches {
		index := findChannelIndexByUID(getChannelSlice(cfg, match.Kind), match.Upstream.ChannelUID)
		discoveryStarted := triggerDiscoveryForChannel(deps, match.Kind, match.Upstream.ChannelUID)
		results = append(results, AutoAddChannelResult{
			AccountUID: match.Upstream.AccountUID, ChannelKind: match.Kind, ChannelUID: match.Upstream.ChannelUID,
			Index: index, Name: match.Upstream.Name, ServiceType: match.Upstream.ServiceType,
			DiscoveryStarted: discoveryStarted,
		})
	}
	primary := primaryAutoAddResult(results, requestKind)
	httpbridge.JSON(c, http.StatusOK, AutoAddResponse{
		AccountUID: primary.AccountUID, ChannelUID: primary.ChannelUID, Index: primary.Index,
		DiscoveryStarted: primary.DiscoveryStarted, Channels: results,
	})
}

func appendAPIKeyToExistingChannel(cfgManager *config.ConfigManager, kind string, index int, apiKey string) error {
	switch kind {
	case "messages":
		return cfgManager.AddAPIKey(index, apiKey)
	case "chat":
		return cfgManager.AddChatAPIKey(index, apiKey)
	case "responses":
		return cfgManager.AddResponsesAPIKey(index, apiKey)
	case "gemini":
		return cfgManager.AddGeminiAPIKey(index, apiKey)
	case "images":
		return cfgManager.AddImagesAPIKey(index, apiKey)
	case "vectors":
		return cfgManager.AddVectorsAPIKey(index, apiKey)
	default:
		return fmt.Errorf("不支持的渠道类型: %s", kind)
	}
}

func seedAutoAddRateLimitHint(
	deps *AutoManagedDeps,
	channels []config.AccountChannel,
	hint *AutoAddRateLimitHint,
	probeAPIKey string,
	probeBaseURL string,
) {
	if deps == nil || deps.RateLimitDiscoverer == nil || hint == nil || strings.TrimSpace(probeAPIKey) == "" {
		return
	}
	rpm := hint.EffectiveRPM
	if rpm <= 0 {
		return
	}
	if hint.InitialRPM > 0 && rpm > hint.InitialRPM {
		rpm = hint.InitialRPM
	}
	if rpm > 120 {
		rpm = 120
	}
	rateLimited := hint.RateLimited || hint.RateLimitedCount > 0
	cfg := deps.CfgManager.GetConfig()

	for _, channel := range channels {
		upstream := channel.Upstream
		containsProbeKey := false
		for _, apiKey := range upstream.APIKeys {
			if apiKey == probeAPIKey {
				containsProbeKey = true
				break
			}
		}
		if !containsProbeKey {
			continue
		}

		baseURL := upstream.BoundBaseURLForKey(probeAPIKey)
		if baseURL == "" {
			baseURL = utils.CanonicalBaseURL(probeBaseURL, upstream.ServiceType)
		}
		if baseURL == "" {
			continue
		}
		endpointUID := GenerateEndpointUID(upstream.ChannelUID, baseURL, KeyHashFromAPIKey(probeAPIKey))
		suggestion := deps.RateLimitDiscoverer.SeedProbeEstimate(endpointUID, rpm, rateLimited)
		if suggestion.RPM <= 0 || deps.Runner == nil || deps.Runner.store == nil {
			continue
		}

		profile := KeyEndpointProfile{}
		if existing := deps.Runner.store.Get(endpointUID); existing != nil {
			profile = *existing
		}
		profile.AccountUID = upstream.AccountUID
		profile.ChannelUID = upstream.ChannelUID
		profile.ChannelID = findChannelIndexByUID(getChannelSlice(cfg, channel.Kind), upstream.ChannelUID)
		profile.ChannelKind = channel.Kind
		profile.EndpointUID = endpointUID
		profile.ServiceType = upstream.ServiceType
		profile.BaseURL = baseURL
		profile.IdentityBaseURL = utils.MetricsIdentityBaseURL(baseURL, upstream.ServiceType)
		profile.KeyMask = utils.MaskAPIKey(probeAPIKey)
		profile.KeyHash = KeyHashFromAPIKey(probeAPIKey)
		profile.CredentialUID = upstream.CredentialUIDForKey(probeAPIKey)
		profile.MetricsKey = computeMetricsIdentityKey(baseURL, probeAPIKey, upstream.ServiceType)
		profile.DiscoveredRPM = suggestion.RPM
		profile.DiscoveredMaxConcurrent = suggestion.MaxConcurrent
		profile.RateLimitConfidence = suggestion.Confidence
		if suggestion.ConcurrentConfidence > profile.RateLimitConfidence {
			profile.RateLimitConfidence = suggestion.ConcurrentConfidence
		}
		profile.RateLimitSource = string(suggestion.Source)
		profile.SuggestedRPMSource = string(suggestion.Source)
		profile.SuggestedRPMTPM = suggestion.TPM
		profile.SuggestedRPMRPD = suggestion.RPD
		if profile.HealthState == "" {
			profile.HealthState = HealthStateUnknown
		}
		if profile.QualityTier == "" {
			profile.QualityTier = QualityTierNormal
		}
		if profile.StabilityTier == "" {
			profile.StabilityTier = StabilityTierNormal
		}
		if profile.SpeedTier == "" {
			profile.SpeedTier = SpeedTierNormal
		}
		if profile.CostTier == "" {
			profile.CostTier = CostTierNormal
		}
		if profile.Source == "" {
			profile.Source = "channel_discovery"
		}
		profile.UpdatedAt = time.Now()
		if err := deps.Runner.store.Upsert(&profile); err != nil {
			log.Printf("[AutoManaged-RateLimit] 写入探测限速画像失败 endpoint=%s: %v", endpointUID, err)
		}
	}
}

func respondAutoAddAccountChannels(c fiber.Ctx, deps *AutoManagedDeps, requestKind, accountUID string) {
	channels := deps.CfgManager.GetAccountChannels(accountUID)
	cfg := deps.CfgManager.GetConfig()
	results := make([]AutoAddChannelResult, 0, len(channels))
	for _, channel := range channels {
		index := findChannelIndexByUID(getChannelSlice(cfg, channel.Kind), channel.Upstream.ChannelUID)
		discoveryStarted := triggerDiscoveryForChannel(deps, channel.Kind, channel.Upstream.ChannelUID)
		results = append(results, AutoAddChannelResult{
			AccountUID: accountUID, ChannelKind: channel.Kind, ChannelUID: channel.Upstream.ChannelUID,
			Index: index, Name: channel.Upstream.Name, ServiceType: channel.Upstream.ServiceType,
			DiscoveryStarted: discoveryStarted,
		})
	}
	primary := primaryAutoAddResult(results, requestKind)
	httpbridge.JSON(c, http.StatusOK, AutoAddResponse{
		AccountUID: accountUID, ChannelUID: primary.ChannelUID, Index: primary.Index,
		DiscoveryStarted: primary.DiscoveryStarted, Channels: results,
	})
}

func normalizeCustomAutoAddRoutes(requestKind string, requested []AutoAddRouteRequest) ([]AutoAddRouteRequest, error) {
	if len(requested) == 0 {
		return []AutoAddRouteRequest{{ChannelKind: requestKind}}, nil
	}
	seen := make(map[string]bool, len(requested))
	routes := make([]AutoAddRouteRequest, 0, len(requested))
	for _, route := range requested {
		kind := strings.TrimSpace(route.ChannelKind)
		if !validChannelKinds[kind] {
			return nil, fmt.Errorf("不支持的渠道类型: %s", kind)
		}
		if len(requested) > 1 && (kind == "images" || kind == "vectors") {
			return nil, fmt.Errorf("%s 渠道不支持多协议自动添加", kind)
		}
		if seen[kind] {
			continue
		}
		seen[kind] = true
		routes = append(routes, AutoAddRouteRequest{
			ChannelKind:     kind,
			SupportedModels: uniqueNonEmptyStrings(route.SupportedModels),
		})
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("routes 不能为空")
	}
	return routes, nil
}

func primaryAutoAddResult(results []AutoAddChannelResult, requestKind string) AutoAddChannelResult {
	for _, result := range results {
		if result.ChannelKind == requestKind {
			return result
		}
	}
	if len(results) > 0 {
		return results[0]
	}
	return AutoAddChannelResult{}
}
