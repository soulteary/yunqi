package autopilot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/utils"
)

func handleRenameAccount(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "无效的请求体"})
			return nil
		}
		accountUID := strings.TrimSpace(httpbridge.Param(c, "accountUid"))
		if err := deps.CfgManager.RenameManagedAccountAudited(managementAuditContext, accountUID, req.Name); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"accountUid": accountUID, "name": strings.TrimSpace(req.Name)})
		return nil
	}
}

func handleDeleteAccount(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		accountUID := strings.TrimSpace(httpbridge.Param(c, "accountUid"))
		channels := deps.CfgManager.GetAccountChannels(accountUID)
		if len(channels) == 0 {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "账号不存在"})
			return nil
		}
		// 只级联删除自动托管渠道；非托管渠道保留并解除账号关联，避免误删用户手工渠道。
		removed, skipped, err := deps.CfgManager.DeleteAccountChannelsAudited(managementAuditContext, accountUID)
		if err != nil {
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": err.Error()})
			return nil
		}
		if deps.Runner != nil && deps.Runner.store != nil {
			if err := deps.Runner.store.DeleteByAccount(accountUID); err != nil {
				log.Printf("[AutoManaged-Delete] 清理账号画像失败 account=%s: %v", accountUID, err)
			}
			for _, channelUID := range removed {
				for _, profile := range deps.Runner.store.ListByChannel(channelUID) {
					if err := deps.Runner.store.Delete(profile.EndpointUID); err != nil {
						log.Printf("[AutoManaged-Delete] 清理旧渠道画像失败 channel=%s endpoint=%s: %v", channelUID, profile.EndpointUID, err)
					}
				}
			}
		}
		log.Printf("[AutoManaged-Delete] 已删除自动托管账号 account=%s deletedChannels=%d skippedNonManaged=%d", accountUID, len(removed), len(skipped))
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"accountUid":        accountUID,
			"deletedChannels":   len(removed),
			"skippedChannels":   skipped,
			"skippedChannelNum": len(skipped),
		})
		return nil
	}
}

type updateAccountRequest struct {
	Name    string   `json:"name"`
	APIKeys []string `json:"apiKeys"`
	// BaseURLs 可选：仅自定义自动托管账号支持手工维护地址池。
	// 省略或为空时表示"不修改地址"，沿用渠道现有地址，保证旧客户端向后兼容。
	BaseURLs []string `json:"baseUrls,omitempty"`
	// Remark 可选指针：省略（nil）表示不修改备注；传空串为显式清空。
	Remark *string `json:"remark,omitempty"`
	// SkipVerify 为 true 时跳过新增 key 的上游连通性验证，
	// 用于验证失败弹窗中用户确认"仍要保存"后的重试。
	SkipVerify bool `json:"skipVerify,omitempty"`
}

type updateAccountResponse struct {
	AccountUID       string `json:"accountUid"`
	KeyCount         int    `json:"keyCount"`
	ChannelCount     int    `json:"channelCount"`
	DiscoveryStarted int    `json:"discoveryStarted"`
	// Warnings 携带降级放行的验证警告（非鉴权类探测失败，key 已保存但连通性未确认）。
	Warnings []string `json:"warnings,omitempty"`
}

// respondAccountUpdateError 输出账号更新错误；新增 key 验证失败时附加结构化标识，
// 供前端识别并给出"仍要保存"入口。
func respondAccountUpdateError(c fiber.Ctx, status int, err error) {
	payload := fiber.Map{"error": err.Error()}
	var kvErr *KeyVerifyError
	if errors.As(err, &kvErr) {
		payload["verifyFailure"] = true
		payload["authFailed"] = kvErr.AuthFailed
	}
	httpbridge.JSON(c, status, payload)
}

// handleUpdateAccount 在账号范围原子替换凭证集合，并只为新增 Key 探测各协议 route。
func handleUpdateAccount(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req updateAccountRequest
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "无效的请求体"})
			return nil
		}
		response, status, err := applyManagedAccountUpdate(httpbridge.Request(c).Context(), deps, strings.TrimSpace(httpbridge.Param(c, "accountUid")), req)
		if err != nil {
			respondAccountUpdateError(c, status, err)
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, response)
		return nil
	}
}

func applyManagedAccountUpdate(ctx context.Context, deps *AutoManagedDeps, accountUID string, req updateAccountRequest) (updateAccountResponse, int, error) {
	if deps == nil || deps.CfgManager == nil {
		return updateAccountResponse{}, http.StatusServiceUnavailable, fmt.Errorf("配置管理器不可用")
	}
	req.APIKeys = uniqueNonEmptyStrings(req.APIKeys)
	if len(req.APIKeys) == 0 {
		return updateAccountResponse{}, http.StatusBadRequest, fmt.Errorf("apiKeys 不能为空")
	}
	channels := deps.CfgManager.GetAccountChannels(accountUID)
	if len(channels) == 0 {
		return updateAccountResponse{}, http.StatusNotFound, fmt.Errorf("账号不存在")
	}
	providerID := channels[0].Upstream.ProviderID
	tmpl, ok := config.GetProviderTemplate(providerID)
	var updates []config.AccountChannelUpdate
	var status int
	var err error
	if ok && providerID != "" {
		// 官方 provider 托管渠道的地址由模板与自动发现统一管理，
		// 故此处显式丢弃请求里的 baseUrls（而非报错），避免手工地址覆盖模板地址，同时保持 API 向后兼容。
		if len(req.BaseURLs) > 0 {
			log.Printf("[AutoManaged-Update] 忽略官方 provider 托管账号的手工 baseUrls account=%s provider=%s count=%d", accountUID, providerID, len(req.BaseURLs))
			req.BaseURLs = nil
		}
		updates, status, err = planManagedAccountUpdates(ctx, accountUID, req, channels, tmpl, len(channels))
	} else if providerID == "" {
		updates, status, err = planCustomManagedAccountUpdates(accountUID, req, channels, len(channels))
	} else {
		return updateAccountResponse{}, http.StatusBadRequest, fmt.Errorf("provider %s 模板不存在", providerID)
	}
	if err != nil {
		return updateAccountResponse{}, status, err
	}
	var verifyWarnings []string
	if providerID == "" && !req.SkipVerify {
		warnings, err := verifyAddedKeysForUpdates(ctx, deps, channels, updates)
		if err != nil {
			return updateAccountResponse{}, http.StatusBadRequest, err
		}
		verifyWarnings = warnings
	}
	changed := channelsWithAddedKeys(channels, req.APIKeys)
	if err := deps.CfgManager.UpdateAccountChannels(accountUID, updates); err != nil {
		return updateAccountResponse{}, http.StatusInternalServerError, err
	}
	resetChangedChannelMetrics(deps, changed)
	started := 0
	for _, accountChannel := range channels {
		if triggerDiscoveryForChannel(deps, accountChannel.Kind, accountChannel.Upstream.ChannelUID) {
			started++
		}
	}
	return updateAccountResponse{AccountUID: accountUID, KeyCount: len(req.APIKeys), ChannelCount: len(channels), DiscoveryStarted: started, Warnings: verifyWarnings}, http.StatusOK, nil
}

// canonicalizeBaseURLs 按 serviceType 规范化地址池：丢弃空值并按 canonical 形式去重，保持输入顺序。
func canonicalizeBaseURLs(rawURLs []string, serviceType string) []string {
	if len(rawURLs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(rawURLs))
	result := make([]string, 0, len(rawURLs))
	for _, rawURL := range rawURLs {
		canonical := utils.CanonicalBaseURL(rawURL, serviceType)
		if canonical == "" {
			continue
		}
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	return result
}

func planCustomManagedAccountUpdates(
	accountUID string,
	req updateAccountRequest,
	channels []config.AccountChannel,
	totalRouteCount int,
) ([]config.AccountChannelUpdate, int, error) {
	if len(channels) == 0 {
		return nil, http.StatusNotFound, fmt.Errorf("账号不存在")
	}
	req.APIKeys = uniqueNonEmptyStrings(req.APIKeys)
	if len(req.APIKeys) == 0 {
		return nil, http.StatusBadRequest, fmt.Errorf("apiKeys 不能为空")
	}
	baseName := strings.TrimSpace(req.Name)
	if baseName == "" {
		baseName = strings.TrimSuffix(channels[0].Upstream.Name, accountRouteSuffix(channels[0].Kind))
	}
	multiRoute := totalRouteCount > 1
	updates := make([]config.AccountChannelUpdate, 0, len(channels))
	for _, accountChannel := range channels {
		channel := accountChannel.Upstream
		if !channel.AutoManaged || channel.ProviderID != "" {
			return nil, http.StatusConflict, fmt.Errorf("账号包含非自定义自动托管渠道")
		}
		existing := make(map[string]config.APIKeyConfig, len(channel.APIKeyConfigs))
		for _, keyConfig := range channel.APIKeyConfigs {
			existing[keyConfig.Key] = keyConfig
		}
		// canonical 形式依赖 serviceType，因此按每条渠道各自规范化请求地址池。
		baseURLs := channel.GetAllBaseURLs()
		if len(req.BaseURLs) > 0 {
			requested := canonicalizeBaseURLs(req.BaseURLs, channel.ServiceType)
			if len(requested) == 0 {
				return nil, http.StatusBadRequest, fmt.Errorf("baseUrls 全部无效，请填写至少一个合法的上游地址")
			}
			baseURLs = requested
		}
		inPool := make(map[string]struct{}, len(baseURLs))
		for _, baseURL := range baseURLs {
			inPool[baseURL] = struct{}{}
		}
		configs := make([]config.APIKeyConfig, 0, len(req.APIKeys))
		for _, key := range req.APIKeys {
			keyConfig, exists := existing[key]
			if !exists {
				// 新增 Key 沿用既有行为：绑定到地址池首个地址。
				keyConfig = config.APIKeyConfig{Key: key}
				if len(baseURLs) > 0 {
					keyConfig.BaseURL = baseURLs[0]
				}
			} else if bound := utils.CanonicalBaseURL(keyConfig.BaseURL, channel.ServiceType); bound != "" {
				// 已绑定地址仍在池内则保持不变（写 canonical 值以与 channel.BaseURLs 对齐）；
				// 已被移出地址池则迁移到首个地址。BaseURL 为空表示未绑定，保持为空，运行时使用完整地址池。
				if _, ok := inPool[bound]; ok {
					keyConfig.BaseURL = bound
				} else if len(baseURLs) > 0 {
					keyConfig.BaseURL = baseURLs[0]
				}
			}
			if keyConfig.CredentialUID == "" {
				keyConfig.CredentialUID = config.GenerateCredentialUID(accountUID, key)
			}
			configs = append(configs, keyConfig)
		}
		updates = append(updates, config.AccountChannelUpdate{
			ChannelUID:   channel.ChannelUID,
			Name:         customAutoAddRouteName(baseName, accountChannel.Kind, multiRoute),
			APIKeys:      append([]string(nil), req.APIKeys...),
			APIKeyConfig: configs,
			BaseURLs:     append([]string(nil), baseURLs...),
			Remark:       req.Remark,
		})
	}
	return updates, http.StatusOK, nil
}

func planManagedAccountUpdates(
	ctx context.Context,
	accountUID string,
	req updateAccountRequest,
	channels []config.AccountChannel,
	tmpl *config.ProviderTemplate,
	totalRouteCount int,
) ([]config.AccountChannelUpdate, int, error) {
	if len(channels) == 0 || tmpl == nil {
		return nil, http.StatusNotFound, fmt.Errorf("账号不存在")
	}
	req.APIKeys = uniqueNonEmptyStrings(req.APIKeys)
	if len(req.APIKeys) == 0 {
		return nil, http.StatusBadRequest, fmt.Errorf("apiKeys 不能为空")
	}
	providerID := channels[0].Upstream.ProviderID
	baseName := strings.TrimSpace(req.Name)
	if baseName == "" {
		baseName = strings.TrimSuffix(channels[0].Upstream.Name, accountRouteSuffix(channels[0].Kind))
	}
	updates := make([]config.AccountChannelUpdate, 0, len(channels))
	for _, accountChannel := range channels {
		channel := accountChannel.Upstream
		if !channel.AutoManaged || channel.ProviderID != providerID {
			return nil, http.StatusConflict, fmt.Errorf("账号包含非托管渠道或 provider 不一致")
		}
		route, found := providerRouteForChannel(tmpl, accountChannel.Kind, channel.ServiceType)
		if !found {
			return nil, http.StatusConflict, fmt.Errorf("provider %s 缺少 %s route", providerID, accountChannel.Kind)
		}
		existing := make(map[string]config.APIKeyConfig, len(channel.APIKeyConfigs))
		for _, keyConfig := range channel.APIKeyConfigs {
			existing[keyConfig.Key] = keyConfig
		}
		var added []string
		for _, key := range req.APIKeys {
			if _, exists := existing[key]; !exists {
				added = append(added, key)
			}
		}
		if len(added) > 0 {
			verified, _, err := verifyProviderRouteKeys(ctx, tmpl, route, added)
			if err != nil {
				return nil, http.StatusBadRequest, err
			}
			for _, keyConfig := range verified {
				existing[keyConfig.Key] = keyConfig
			}
		}
		configs := make([]config.APIKeyConfig, 0, len(req.APIKeys))
		baseURLs := make([]string, 0, len(req.APIKeys))
		for _, key := range req.APIKeys {
			keyConfig, exists := existing[key]
			if !exists {
				keyConfig = config.APIKeyConfig{Key: key, BaseURL: channel.BoundBaseURLForKey(key)}
			}
			if keyConfig.CredentialUID == "" {
				keyConfig.CredentialUID = config.GenerateCredentialUID(accountUID, key)
			}
			configs = append(configs, keyConfig)
			if keyConfig.BaseURL != "" {
				baseURLs = append(baseURLs, keyConfig.BaseURL)
			}
		}
		updates = append(updates, config.AccountChannelUpdate{
			ChannelUID: channel.ChannelUID, Name: providerRouteName(baseName, route, totalRouteCount > 1),
			APIKeys: append([]string(nil), req.APIKeys...), APIKeyConfig: configs, BaseURLs: uniqueNonEmptyStrings(baseURLs),
			Remark: req.Remark,
		})
	}
	return updates, http.StatusOK, nil
}

func handleAddAccountCredentials(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		var req struct {
			APIKeys []string `json:"apiKeys"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "无效的请求体"})
			return nil
		}
		accountUID := strings.TrimSpace(httpbridge.Param(c, "accountUid"))
		channels := deps.CfgManager.GetAccountChannels(accountUID)
		if len(channels) == 0 {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "账号不存在"})
			return nil
		}
		desired := append([]string(nil), channels[0].Upstream.APIKeys...)
		desired = append(desired, req.APIKeys...)
		response, status, err := applyManagedAccountUpdate(httpbridge.Request(c).Context(), deps, accountUID, updateAccountRequest{APIKeys: desired})
		if err != nil {
			respondAccountUpdateError(c, status, err)
			return nil
		}
		httpbridge.JSON(c, http.StatusCreated, response)
		return nil
	}
}

func handlePatchAccountCredentials(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		var req struct {
			AddAPIKeys           []string `json:"addApiKeys"`
			RemoveCredentialUIDs []string `json:"removeCredentialUids"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "无效的请求体"})
			return nil
		}
		accountUID := strings.TrimSpace(httpbridge.Param(c, "accountUid"))
		channels := deps.CfgManager.GetAccountChannels(accountUID)
		if len(channels) == 0 {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "账号不存在"})
			return nil
		}
		removeSet := make(map[string]bool, len(req.RemoveCredentialUIDs))
		for _, uid := range req.RemoveCredentialUIDs {
			removeSet[strings.TrimSpace(uid)] = true
		}
		removedKeys := make(map[string]string)
		var desired []string
		for _, keyConfig := range channels[0].Upstream.APIKeyConfigs {
			if removeSet[keyConfig.CredentialUID] {
				removedKeys[keyConfig.CredentialUID] = keyConfig.Key
				continue
			}
			desired = append(desired, keyConfig.Key)
		}
		if len(removedKeys) != len(removeSet) {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "包含不存在的 credentialUid"})
			return nil
		}
		desired = append(desired, req.AddAPIKeys...)
		if len(uniqueNonEmptyStrings(desired)) == 0 {
			httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": "账号至少需要保留一个 Key"})
			return nil
		}
		response, status, err := applyManagedAccountUpdate(httpbridge.Request(c).Context(), deps, accountUID, updateAccountRequest{APIKeys: desired})
		if err != nil {
			respondAccountUpdateError(c, status, err)
			return nil
		}
		cleanupRemovedCredentialProfiles(deps, accountUID, channels, removedKeys)
		httpbridge.JSON(c, http.StatusOK, response)
		return nil
	}
}

func cleanupRemovedCredentialProfiles(deps *AutoManagedDeps, accountUID string, channels []config.AccountChannel, removed map[string]string) {
	if deps == nil || deps.Runner == nil || deps.Runner.store == nil {
		return
	}
	for credentialUID, apiKey := range removed {
		if err := deps.Runner.store.DeleteByCredential(accountUID, credentialUID); err != nil {
			log.Printf("[AutoManaged-CredentialDelete] 清理凭证画像失败 account=%s credential=%s: %v", accountUID, credentialUID, err)
		}
		keyHash := KeyHashFromAPIKey(apiKey)
		for _, channel := range channels {
			for _, profile := range deps.Runner.store.ListByChannel(channel.Upstream.ChannelUID) {
				if profile.CredentialUID == credentialUID || profile.KeyHash == keyHash {
					if err := deps.Runner.store.Delete(profile.EndpointUID); err != nil {
						log.Printf("[AutoManaged-CredentialDelete] 清理旧凭证画像失败 endpoint=%s: %v", profile.EndpointUID, err)
					}
				}
			}
		}
	}
}

func handleDeleteAccountCredential(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		accountUID := strings.TrimSpace(httpbridge.Param(c, "accountUid"))
		credentialUID := strings.TrimSpace(httpbridge.Param(c, "credentialUid"))
		channels := deps.CfgManager.GetAccountChannels(accountUID)
		if len(channels) == 0 {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "账号不存在"})
			return nil
		}
		var removeKey string
		for _, keyConfig := range channels[0].Upstream.APIKeyConfigs {
			if keyConfig.CredentialUID == credentialUID {
				removeKey = keyConfig.Key
				break
			}
		}
		if removeKey == "" {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "凭证不存在"})
			return nil
		}
		var desired []string
		for _, key := range channels[0].Upstream.APIKeys {
			if key != removeKey {
				desired = append(desired, key)
			}
		}
		if len(desired) == 0 {
			httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": "不能删除账号的最后一个 Key，请删除整个账号"})
			return nil
		}
		response, status, err := applyManagedAccountUpdate(httpbridge.Request(c).Context(), deps, accountUID, updateAccountRequest{APIKeys: desired})
		if err != nil {
			respondAccountUpdateError(c, status, err)
			return nil
		}
		cleanupRemovedCredentialProfiles(deps, accountUID, channels, map[string]string{credentialUID: removeKey})
		httpbridge.JSON(c, http.StatusOK, response)
		return nil
	}
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
