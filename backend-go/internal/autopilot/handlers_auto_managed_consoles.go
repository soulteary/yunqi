package autopilot

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/utils"
)

type managedDeepSeekCredentialBalance struct {
	CredentialUID string                `json:"credentialUid"`
	KeyMask       string                `json:"keyMask"`
	IsAvailable   bool                  `json:"isAvailable"`
	BalanceInfos  []DeepSeekBalanceInfo `json:"balanceInfos,omitempty"`
	FetchedAt     time.Time             `json:"fetchedAt"`
	Error         string                `json:"error,omitempty"`
}

func handleDeepSeekBalance(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		accountUID := strings.TrimSpace(httpbridge.Param(c, "accountUid"))
		cfg := deps.CfgManager.GetConfig()
		var account *config.ManagedAccountConfig
		for i := range cfg.ManagedAccounts {
			if cfg.ManagedAccounts[i].AccountUID == accountUID {
				account = &cfg.ManagedAccounts[i]
				break
			}
		}
		if account == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "账号不存在"})
			return nil
		}
		if account.ProviderID != "deepseek" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "仅 DeepSeek 自动托管账号支持余额查询"})
			return nil
		}

		client := deps.DeepSeekClient
		if client == nil {
			client = NewDeepSeekClient(nil)
		}
		ctx, cancel := context.WithTimeout(httpbridge.Request(c).Context(), deepSeekRequestTimeout)
		defer cancel()
		balances := make([]managedDeepSeekCredentialBalance, len(account.Credentials))
		workers := make(chan struct{}, deepSeekBalanceWorkers)
		var wg sync.WaitGroup
		for i, credential := range account.Credentials {
			wg.Add(1)
			go func(index int, credential config.ManagedAccountCredential) {
				defer wg.Done()
				select {
				case workers <- struct{}{}:
					defer func() { <-workers }()
				case <-ctx.Done():
					balances[index] = managedDeepSeekCredentialBalance{
						CredentialUID: credential.CredentialUID,
						KeyMask:       utils.MaskAPIKey(credential.APIKey),
						FetchedAt:     time.Now().UTC(),
						Error:         "DeepSeek 余额查询超时",
					}
					return
				}
				balance, err := client.FetchBalance(ctx, credential.APIKey)
				balances[index] = managedDeepSeekCredentialBalance{
					CredentialUID: credential.CredentialUID,
					KeyMask:       utils.MaskAPIKey(credential.APIKey),
					FetchedAt:     time.Now().UTC(),
				}
				if err != nil {
					balances[index].Error = err.Error()
					return
				}
				balances[index].IsAvailable = balance.IsAvailable
				balances[index].BalanceInfos = balance.BalanceInfos
			}(i, credential)
		}
		wg.Wait()
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"accountUid": accountUID, "balances": balances})
		return nil
	}
}

func handleSetMiMoConsoleCookie(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		var req struct {
			Cookie         string `json:"cookie"`
			AdoptCookieKey bool   `json:"adoptCookieKey"`
		}
		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "无效的请求体"})
			return nil
		}
		accountUID, credentialUID := strings.TrimSpace(httpbridge.Param(c, "accountUid")), strings.TrimSpace(httpbridge.Param(c, "credentialUid"))
		credential, ok := deps.CfgManager.GetManagedAccountCredential(accountUID, credentialUID)
		if !ok {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "推理 Key 凭证不存在"})
			return nil
		}
		channels := deps.CfgManager.GetAccountChannels(accountUID)
		if len(channels) == 0 || channels[0].Upstream.ProviderID != "mimo" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "仅 MiMo 自动托管账号支持绑定控制台 Cookie"})
			return nil
		}
		verification, err := mimoConsoleClient(deps).Verify(httpbridge.Request(c).Context(), req.Cookie)
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		matches := apiKeysEqual(credential.APIKey, verification.APIKey)
		if !matches && !req.AdoptCookieKey {
			httpbridge.JSON(c, http.StatusConflict, fiber.Map{
				"error": "Cookie 所属 Token Plan Key 与当前渠道 Key 不一致",
				"code":  "mimo_cookie_key_mismatch", "currentKeyMask": utils.MaskAPIKey(credential.APIKey),
				"cookieKeyMask": utils.MaskAPIKey(verification.APIKey),
			})
			return nil
		}
		replacementKey := ""
		if !matches {
			replacementKey = verification.APIKey
		}
		if err := deps.CfgManager.BindManagedAccountMiMoConsoleAudited(managementAuditContext, accountUID, credentialUID, replacementKey, verification.Snapshot); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		// Cookie 采用了新 Key 时，旧 Key 不再属于当前账号，不触发恢复。
		if replacementKey == "" {
			config.TryRestoreDisabledKeysByUsage(deps.CfgManager, accountUID, credential.APIKey, credentialUID)
		}
		started := 0
		if replacementKey != "" {
			for _, channel := range deps.CfgManager.GetAccountChannels(accountUID) {
				if triggerDiscoveryForChannel(deps, channel.Kind, channel.Upstream.ChannelUID) {
					started++
				}
			}
		}
		response := fiber.Map{
			"accountUid": accountUID, "credentialUid": credentialUID, "keyAdopted": replacementKey != "",
			"keyMask": utils.MaskAPIKey(verification.APIKey), "tokenPlan": mimoTokenPlanView(&verification.Snapshot),
			"discoveryStarted": started,
		}
		if replacementKey != "" {
			response["adoptedApiKey"] = replacementKey
		}
		httpbridge.JSON(c, http.StatusOK, response)
		return nil
	}
}

func handleRefreshMiMoConsoleCookie(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		accountUID, credentialUID := strings.TrimSpace(httpbridge.Param(c, "accountUid")), strings.TrimSpace(httpbridge.Param(c, "credentialUid"))
		credential, ok := deps.CfgManager.GetManagedAccountCredential(accountUID, credentialUID)
		if !ok || credential.MiMoConsole == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "未绑定 MiMo 控制台 Cookie"})
			return nil
		}
		verification, err := mimoConsoleClient(deps).Verify(httpbridge.Request(c).Context(), credential.MiMoConsole.Cookie)
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		if !apiKeysEqual(credential.APIKey, verification.APIKey) {
			httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": "Cookie 所属 Key 已变化，请重新绑定并确认是否采用新 Key", "code": "mimo_cookie_key_mismatch"})
			return nil
		}
		if err := deps.CfgManager.BindManagedAccountMiMoConsoleAudited(managementAuditContext, accountUID, credentialUID, "", verification.Snapshot); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		config.TryRestoreDisabledKeysByUsage(deps.CfgManager, accountUID, credential.APIKey, credentialUID)
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"tokenPlan": mimoTokenPlanView(&verification.Snapshot)})
		return nil
	}
}

func handleClearMiMoConsoleCookie(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		if err := deps.CfgManager.ClearManagedAccountMiMoConsoleAudited(managementAuditContext, strings.TrimSpace(httpbridge.Param(c, "accountUid")), strings.TrimSpace(httpbridge.Param(c, "credentialUid"))); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.Status(c, http.StatusNoContent)
		return nil
	}
}

func mimoConsoleClient(deps *AutoManagedDeps) *MiMoConsoleClient {
	if deps != nil && deps.MiMoConsoleClient != nil {
		return deps.MiMoConsoleClient
	}
	return &MiMoConsoleClient{HTTPClient: &http.Client{Timeout: 10 * time.Second}}
}

func handleSetCompshareConsoleCookie(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		var req struct {
			Cookie string `json:"cookie"`
		}
		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "无效的请求体"})
			return nil
		}
		accountUID, credentialUID := strings.TrimSpace(httpbridge.Param(c, "accountUid")), strings.TrimSpace(httpbridge.Param(c, "credentialUid"))
		credential, ok := deps.CfgManager.GetManagedAccountCredential(accountUID, credentialUID)
		if !ok {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "推理 Key 凭证不存在"})
			return nil
		}
		channels := deps.CfgManager.GetAccountChannels(accountUID)
		if len(channels) == 0 || channels[0].Upstream.ProviderID != "compshare" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "仅优云智算自动托管账号支持绑定控制台 Cookie"})
			return nil
		}
		snapshot, err := compshareConsoleClient(deps).Verify(httpbridge.Request(c).Context(), req.Cookie, credential.APIKey)
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		if err := deps.CfgManager.BindManagedAccountCompshareConsoleAudited(managementAuditContext, accountUID, credentialUID, *snapshot); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		config.TryRestoreDisabledKeysByUsage(deps.CfgManager, accountUID, credential.APIKey, credentialUID)
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"accountUid": accountUID, "credentialUid": credentialUID, "plan": compsharePlanView(snapshot),
		})
		return nil
	}
}

func handleRefreshCompshareConsoleCookie(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		accountUID, credentialUID := strings.TrimSpace(httpbridge.Param(c, "accountUid")), strings.TrimSpace(httpbridge.Param(c, "credentialUid"))
		credential, ok := deps.CfgManager.GetManagedAccountCredential(accountUID, credentialUID)
		if !ok || credential.CompshareConsole == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "未绑定优云智算控制台 Cookie"})
			return nil
		}
		snapshot, err := compshareConsoleClient(deps).Verify(httpbridge.Request(c).Context(), credential.CompshareConsole.Cookie, credential.APIKey)
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		if err := deps.CfgManager.BindManagedAccountCompshareConsoleAudited(managementAuditContext, accountUID, credentialUID, *snapshot); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		config.TryRestoreDisabledKeysByUsage(deps.CfgManager, accountUID, credential.APIKey, credentialUID)
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"plan": compsharePlanView(snapshot)})
		return nil
	}
}

func handleClearCompshareConsoleCookie(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		if err := deps.CfgManager.ClearManagedAccountCompshareConsoleAudited(managementAuditContext, strings.TrimSpace(httpbridge.Param(c, "accountUid")), strings.TrimSpace(httpbridge.Param(c, "credentialUid"))); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.Status(c, http.StatusNoContent)
		return nil
	}
}

func compshareConsoleClient(deps *AutoManagedDeps) *CompshareConsoleClient {
	if deps != nil && deps.CompshareConsoleClient != nil {
		return deps.CompshareConsoleClient
	}
	return &CompshareConsoleClient{HTTPClient: &http.Client{Timeout: 10 * time.Second}}
}

func handleSetKimiConsoleToken(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		var req struct {
			AccessToken string `json:"accessToken"`
		}
		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "无效的请求体"})
			return nil
		}
		accountUID, credentialUID := strings.TrimSpace(httpbridge.Param(c, "accountUid")), strings.TrimSpace(httpbridge.Param(c, "credentialUid"))
		credential, ok := deps.CfgManager.GetManagedAccountCredential(accountUID, credentialUID)
		if !ok {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "推理 Key 凭证不存在"})
			return nil
		}
		channels := deps.CfgManager.GetAccountChannels(accountUID)
		if len(channels) == 0 || channels[0].Upstream.ProviderID != "kimi" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "仅 Kimi 自动托管账号支持绑定控制台令牌"})
			return nil
		}
		console, err := kimiConsoleClient(deps).Verify(httpbridge.Request(c).Context(), req.AccessToken)
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		if err := deps.CfgManager.BindManagedAccountKimiConsoleAudited(managementAuditContext, accountUID, credentialUID, *console); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		config.TryRestoreDisabledKeysByUsage(deps.CfgManager, accountUID, credential.APIKey, credentialUID)
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"accountUid": accountUID, "credentialUid": credentialUID, "usage": console.Usage,
		})
		return nil
	}
}

func handleRefreshKimiConsoleToken(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		accountUID, credentialUID := strings.TrimSpace(httpbridge.Param(c, "accountUid")), strings.TrimSpace(httpbridge.Param(c, "credentialUid"))
		credential, ok := deps.CfgManager.GetManagedAccountCredential(accountUID, credentialUID)
		if !ok || credential.KimiConsole == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "未绑定 Kimi 控制台令牌"})
			return nil
		}
		console, err := kimiConsoleClient(deps).Verify(httpbridge.Request(c).Context(), credential.KimiConsole.AccessToken)
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		if err := deps.CfgManager.BindManagedAccountKimiConsoleAudited(managementAuditContext, accountUID, credentialUID, *console); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		config.TryRestoreDisabledKeysByUsage(deps.CfgManager, accountUID, credential.APIKey, credentialUID)
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"usage": console.Usage})
		return nil
	}
}

func handleClearKimiConsoleToken(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		if err := deps.CfgManager.ClearManagedAccountKimiConsoleAudited(managementAuditContext,
			strings.TrimSpace(httpbridge.Param(c, "accountUid")), strings.TrimSpace(httpbridge.Param(c, "credentialUid")),
		); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.Status(c, http.StatusNoContent)
		return nil
	}
}

func kimiConsoleClient(deps *AutoManagedDeps) *KimiConsoleClient {
	if deps != nil && deps.KimiConsoleClient != nil {
		return deps.KimiConsoleClient
	}
	return &KimiConsoleClient{HTTPClient: &http.Client{Timeout: 10 * time.Second}}
}
