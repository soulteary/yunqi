package autopilot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

var errAccountMissing = errors.New("subscription account not found")

// NewApiAccountCreateRequest POST /api/subscriptions/:uid/accounts 请求体。
type NewApiAccountCreateRequest struct {
	AccessToken                string   `json:"accessToken" binding:"required"`
	UserID                     string   `json:"userId,omitempty"`
	DisplayName                string   `json:"displayName,omitempty"`
	AuthTokenMode              string   `json:"authTokenMode,omitempty"`
	ProvisionModels            []string `json:"provisionModels,omitempty"`
	MaxGroupMultiplier         *float64 `json:"maxGroupMultiplier,omitempty"`
	ProvisionAllEligibleGroups bool     `json:"provisionAllEligibleGroups,omitempty"`
	// ProxyURL 账号级代理覆盖，空=继承订阅级代理设置。
	ProxyURL string `json:"proxyUrl,omitempty"`
	// ProxyPreferDirect 直连优先，仅在代理生效时有意义。
	ProxyPreferDirect bool `json:"proxyPreferDirect,omitempty"`
}

// resolveNewApiAccountProxy 解析账号生效的代理设置：账号级优先，缺省继承调用方
// 传入的生效值（渠道级"代理通道"优先，回退订阅级）。
func resolveNewApiAccountProxy(account NewApiAccount, fallbackURL string, fallbackDirect bool) (string, bool) {
	if trimmed := strings.TrimSpace(account.ProxyURL); trimmed != "" {
		return trimmed, account.ProxyPreferDirect
	}
	return fallbackURL, fallbackDirect
}

// adapterForAccount 按账号生效代理构造适配器：账号级覆盖优先，
// 缺省继承渠道级"代理通道"，关联渠道均未配置时回退订阅级。
func adapterForAccount(deps *NewApiRouteDeps, profile *SubscriptionProfile, account NewApiAccount) *NewApiAdapter {
	fallbackURL, fallbackDirect := profile.ProxyURL, profile.ProxyPreferDirect
	if deps != nil && deps.SyncService != nil {
		fallbackURL, fallbackDirect = deps.SyncService.effectiveProxyFor(profile)
	}
	proxyURL, preferDirect := resolveNewApiAccountProxy(account, fallbackURL, fallbackDirect)
	return NewApiAdapterForProxy(proxyURL, preferDirect)
}

// NewApiCredentialsUpdateRequest PATCH /api/subscriptions/:uid/newapi-credentials 请求体。
// 指针字段用于区分“不修改”与显式提交；accessToken 只接收不回显。
type NewApiCredentialsUpdateRequest struct {
	AccessToken       *string `json:"accessToken,omitempty"`
	UserID            *string `json:"userId,omitempty"`
	AuthTokenMode     *string `json:"authTokenMode,omitempty"`
	ProxyURL          *string `json:"proxyUrl,omitempty"`
	ProxyPreferDirect *bool   `json:"proxyPreferDirect,omitempty"`
	ExpectedVersion   *uint64 `json:"expectedVersion,omitempty"`
}

// NewApiAccountItem 账号列表响应单条（脱敏）。
type NewApiAccountItem struct {
	AccountUID        string                 `json:"accountUid"`
	UserID            string                 `json:"userId,omitempty"`
	AuthTokenMode     string                 `json:"authTokenMode,omitempty"`
	DisplayName       string                 `json:"displayName,omitempty"`
	Balance           float64                `json:"balance,omitempty"`
	Status            string                 `json:"status,omitempty"`
	AccessTokenMasked string                 `json:"accessTokenMasked,omitempty"`
	ProvisionedKeys   []NewApiProvisionedKey `json:"provisionedKeys,omitempty"`
	LastSyncError     string                 `json:"lastSyncError,omitempty"`
	LastCheckedAt     time.Time              `json:"lastCheckedAt,omitempty"`
	CreatedAt         time.Time              `json:"createdAt"`
	// 账号级代理设置（非敏感配置，空表示继承订阅级）
	ProxyURL          string `json:"proxyUrl,omitempty"`
	ProxyPreferDirect bool   `json:"proxyPreferDirect,omitempty"`
}

// NewApiAccountListResponse GET /api/subscriptions/:uid/accounts 响应体。
type NewApiAccountListResponse struct {
	Accounts []NewApiAccountItem `json:"accounts"`
}

// handleAddSubscriptionAccount 为已有 new-api 订阅添加新账号，并按订阅级倍率上限为其合格分组
// 自动建代理 key 并入关联渠道，使多账号共同分担流量/额度。建 key 失败时回滚远端 key，不留下半成品账号。
func handleAddSubscriptionAccount(deps *NewApiRouteDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		uid := httpbridge.Param(c, "uid")
		if uid == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "subscription uid 不能为空"})
			return nil
		}

		profile := deps.Store.Get(uid)
		if profile == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": fmt.Sprintf("subscription_uid=%s 不存在", uid)})
			return nil
		}
		if profile.Provider != "new_api" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "仅 new_api 类型订阅支持多账号"})
			return nil
		}

		var req NewApiAccountCreateRequest
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "请求参数无效: " + err.Error()})
			return nil
		}
		if req.AccessToken == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "accessToken 必填"})
			return nil
		}

		// 与主 provision 共用订阅 uid 锁：保证同一订阅的添加账号与 provision 串行。
		// 慢速上游 HTTP 与 store 写在锁内完成（同 uid 串行化），避免与 provision 交叉。
		var lock *sync.Mutex
		if deps != nil && deps.SyncService != nil {
			lock = deps.SyncService.LockForUID(uid)
		} else {
			lock = lockForKeyFrom(&newAPIProvisionUIDLocksMu, newAPIProvisionUIDLocks, uid)
		}
		lock.Lock()
		defer lock.Unlock()

		// 探测（校验/分组）与建 key 分开计时：建 key 预算随分组数伸缩，
		// 避免慢速代理下共享小预算在流程中途到期（context deadline exceeded）。
		probeCtx, probeCancel := context.WithTimeout(httpbridge.Request(c).Context(), newApiProbeTimeout)
		defer probeCancel()

		// 代理解析：请求显式携带优先，缺省继承渠道级代理通道（回退订阅级设置）
		proxyURL := strings.TrimSpace(req.ProxyURL)
		proxyPreferDirect := req.ProxyPreferDirect
		if proxyURL == "" {
			if deps != nil && deps.SyncService != nil {
				proxyURL, proxyPreferDirect = deps.SyncService.effectiveProxyFor(profile)
			} else {
				proxyURL, proxyPreferDirect = profile.ProxyURL, profile.ProxyPreferDirect
			}
		}
		adapter := NewApiAdapterForProxy(proxyURL, proxyPreferDirect)
		self, derivedUserID, err := adapter.VerifyWithFallback(probeCtx, profile.BaseURL, req.AccessToken, req.UserID, req.AuthTokenMode)
		if err != nil {
			httpbridge.JSON(c, http.StatusBadGateway, fiber.Map{"error": fmt.Sprintf("账号验证失败: %v", err)})
			return nil
		}

		accountUID := fmt.Sprintf("acct_%d", time.Now().UnixNano())

		// 为该账号建 key：沿用订阅级倍率上限与模型白名单。建 key 失败即整个添加失败并回收已建 key。
		var accountKeys []NewApiProvisionedKey
		if deps.CfgManager != nil {
			groups, gErr := adapter.FetchGroups(probeCtx, profile.BaseURL, req.AccessToken, derivedUserID, req.AuthTokenMode)
			if gErr != nil {
				httpbridge.JSON(c, http.StatusBadGateway, fiber.Map{"error": fmt.Sprintf("无法获取该账号分组倍率，已阻止建 key: %v", gErr)})
				return nil
			}
			// 建 key 阈值：请求显式携带优先，缺省用渠道级上限（真源），最后回退订阅接入初始值。
			maxGroupMultiplier := req.MaxGroupMultiplier
			if maxGroupMultiplier == nil && deps != nil && deps.SyncService != nil {
				maxGroupMultiplier = deps.SyncService.linkedChannelMaxGroupMultiplier(profile)
			}
			if maxGroupMultiplier == nil {
				maxGroupMultiplier = profile.MaxGroupMultiplier
			}
			resolved, rErr := resolveNewApiProvisionGroups(groups, "", req.ProvisionAllEligibleGroups, maxGroupMultiplier)
			if rErr != nil {
				httpbridge.JSON(c, http.StatusUnprocessableEntity, fiber.Map{"error": "分组倍率校验失败: " + rErr.Error()})
				return nil
			}
			provisionModels := req.ProvisionModels
			if len(provisionModels) == 0 {
				provisionModels = profile.ProvisionModels
			}
			// 建 key 阶段单独计时：预算随分组数伸缩（查重+创建+揭示，每个分组多次顺序往返）。
			ctx, cancel := context.WithTimeout(httpbridge.Request(c).Context(), newApiProvisionTimeout(len(resolved)))
			defer cancel()
			provisionReq := NewApiProvisionRequest{
				BaseURL:                    profile.BaseURL,
				AccessToken:                req.AccessToken,
				UserID:                     req.UserID,
				AuthTokenMode:              req.AuthTokenMode,
				ProvisionAllEligibleGroups: true,
				ProvisionModels:            provisionModels,
				MaxGroupMultiplier:         maxGroupMultiplier,
			}
			// key 名直接用 ccx-{分组名}：FindTokenByName 按各账号自己的 token 列表查重，
			// 跨账号同名天然隔离，同名同组自动复用。
			provisioned, skippedEmptyGroups, pErr := provisionNewApiGroupKeys(ctx, adapter, provisionReq, derivedUserID, resolved, "")
			if len(skippedEmptyGroups) > 0 {
				log.Printf("[NewApi-Account] 跳过 0 模型分组 subscription=%s groups=%v", uid, skippedEmptyGroups)
			}
			if pErr != nil {
				var conflict *newApiProvisionConflictError
				if errors.As(pErr, &conflict) {
					httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": "建 key 失败: " + conflict.Error()})
					return nil
				}
				var keyConflict *NewApiProvisionKeyConflictError
				if errors.As(pErr, &keyConflict) {
					httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": "建 key 失败: " + keyConflict.Error()})
					return nil
				}
				httpbridge.JSON(c, http.StatusBadGateway, fiber.Map{"error": fmt.Sprintf("建 key 失败: %v", pErr)})
				return nil
			}
			accountKeys = make([]NewApiProvisionedKey, 0, len(provisioned))
			plaintextByToken := make(map[int64]string, len(provisioned))
			for _, key := range provisioned {
				accountKeys = append(accountKeys, key.NewApiProvisionedKey)
				plaintextByToken[int64(key.TokenID)] = key.Key
			}

			account := NewApiAccount{
				AccountUID:        accountUID,
				AccessToken:       req.AccessToken,
				UserID:            derivedUserID,
				AuthTokenMode:     req.AuthTokenMode,
				ProxyURL:          strings.TrimSpace(req.ProxyURL),
				ProxyPreferDirect: req.ProxyPreferDirect,
				DisplayName:       req.DisplayName,
				Balance:           float64(self.Quota),
				Status:            "active",
				ProvisionedKeys:   accountKeys,
				LastCheckedAt:     time.Now(),
				CreatedAt:         time.Now(),
			}
			if err := deps.Store.AddAccount(uid, account); err != nil {
				cleanupNewApiProvisionedKeys(ctx, adapter, provisionReq, derivedUserID, provisioned)
				httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": err.Error()})
				return nil
			}

			// 把账号 key 明文注入关联渠道。失败时删除账号并回收远端 key。
			if deps.SyncService != nil {
				fresh := deps.Store.Get(uid)
				if rErr := deps.SyncService.ReconcileAccountProvisioned(fresh, accountUID, groups, plaintextByToken); rErr != nil {
					_ = deps.Store.RemoveAccount(uid, accountUID)
					cleanupNewApiProvisionedKeys(ctx, adapter, provisionReq, derivedUserID, provisioned)
					httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": "同步 key 到渠道失败: " + rErr.Error()})
					return nil
				}
			}

			// 无主账号凭证的订阅（主账号被删除后）：首个添加的账号提升为主账号
			// （从 Accounts 移除、凭证迁移到订阅级），使「删除 + 重新添加」成为
			// 更换主凭证的完整路径。失败时回滚账号与远端 key。
			if strings.TrimSpace(profile.AccessToken) == "" {
				promotedAt := time.Now()
				if pErr := deps.Store.Patch(uid, nil, func(p *SubscriptionProfile) error {
					p.AccessToken = req.AccessToken
					p.UserID = derivedUserID
					p.Username = self.Username
					if strings.TrimSpace(req.AuthTokenMode) != "" {
						p.AuthTokenMode = req.AuthTokenMode
					}
					p.ProvisionedKeys = append([]NewApiProvisionedKey(nil), accountKeys...)
					if len(accountKeys) > 0 {
						p.ProvisionedTokenID = accountKeys[0].TokenID
					}
					p.Balance = float64(self.Quota)
					p.UsedQuota = self.UsedQuota
					p.LastBalanceRefreshAt = &promotedAt
					p.LastBalanceRefreshError = ""
					kept := p.Accounts[:0]
					for _, a := range p.Accounts {
						if a.AccountUID != accountUID {
							kept = append(kept, a)
						}
					}
					p.Accounts = kept
					return nil
				}); pErr != nil {
					_ = deps.Store.RemoveAccount(uid, accountUID)
					cleanupNewApiProvisionedKeys(ctx, adapter, provisionReq, derivedUserID, provisioned)
					httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": "提升主账号失败: " + pErr.Error()})
					return nil
				}
			}

			if deps.Runner != nil {
				for _, chUID := range profile.LinkedChannelUIDs {
					_, _, channel, ok := findNewApiChannel(deps.CfgManager, chUID)
					if !ok {
						continue
					}
					ch := channel
					deps.Runner.TriggerDiscovery(chUID, &ch, deps.CfgManager)
				}
			}
			httpbridge.JSON(c, http.StatusCreated, NewApiAccountItem{
				AccountUID:        account.AccountUID,
				UserID:            account.UserID,
				AuthTokenMode:     account.AuthTokenMode,
				DisplayName:       account.DisplayName,
				Balance:           account.Balance,
				Status:            account.Status,
				AccessTokenMasked: maskAccessToken(account.AccessToken),
				ProvisionedKeys:   account.ProvisionedKeys,
				LastCheckedAt:     account.LastCheckedAt,
				CreatedAt:         account.CreatedAt,
				ProxyURL:          account.ProxyURL,
				ProxyPreferDirect: account.ProxyPreferDirect,
			})
			return nil
		}

		// 无 CfgManager（仅 Store 注入，如部分测试）：退化为只登记账号不建 key。
		account := NewApiAccount{
			AccountUID:    accountUID,
			AccessToken:   req.AccessToken,
			UserID:        derivedUserID,
			AuthTokenMode: req.AuthTokenMode,
			DisplayName:   req.DisplayName,
			Balance:       float64(self.Quota),
			Status:        "active",
			LastCheckedAt: time.Now(),
			CreatedAt:     time.Now(),
		}
		if err := deps.Store.AddAccount(uid, account); err != nil {
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, http.StatusCreated, NewApiAccountItem{
			AccountUID:        account.AccountUID,
			UserID:            account.UserID,
			AuthTokenMode:     account.AuthTokenMode,
			DisplayName:       account.DisplayName,
			Balance:           account.Balance,
			Status:            account.Status,
			AccessTokenMasked: maskAccessToken(account.AccessToken),
			LastCheckedAt:     account.LastCheckedAt,
			CreatedAt:         account.CreatedAt,
			ProxyURL:          account.ProxyURL,
			ProxyPreferDirect: account.ProxyPreferDirect,
		})
		return nil
	}
}

// handleListSubscriptionAccounts 获取订阅下的账号列表（脱敏）。
func handleListSubscriptionAccounts(deps *NewApiRouteDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		uid := httpbridge.Param(c, "uid")
		if uid == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "subscription uid 不能为空"})
			return nil
		}

		profile := deps.Store.Get(uid)
		if profile == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": fmt.Sprintf("subscription_uid=%s 不存在", uid)})
			return nil
		}

		items := make([]NewApiAccountItem, 0, len(profile.Accounts))
		for _, acc := range profile.Accounts {
			items = append(items, NewApiAccountItem{
				AccountUID:        acc.AccountUID,
				UserID:            acc.UserID,
				AuthTokenMode:     acc.AuthTokenMode,
				DisplayName:       acc.DisplayName,
				Balance:           acc.Balance,
				Status:            acc.Status,
				AccessTokenMasked: maskAccessToken(acc.AccessToken),
				ProvisionedKeys:   acc.ProvisionedKeys,
				LastSyncError:     acc.LastSyncError,
				LastCheckedAt:     acc.LastCheckedAt,
				CreatedAt:         acc.CreatedAt,
				ProxyURL:          acc.ProxyURL,
				ProxyPreferDirect: acc.ProxyPreferDirect,
			})
		}
		httpbridge.JSON(c, http.StatusOK, NewApiAccountListResponse{Accounts: items})
		return nil
	}
}

// handleDeletePrimaryAccount 删除主账号：账号平权模型下「删除主账号」＝清空订阅级凭证
// 与用户信息、把主账号自动接入 key 从关联渠道剔除并 best-effort 回收远端 key。
// 订阅本体（baseUrl/分组/计费/渠道关联）保留；此后添加账号会把新凭证提升为主账号，
// 「删除 + 重新添加」即更换主凭证的完整路径。
func handleDeletePrimaryAccount(deps *NewApiRouteDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		uid := httpbridge.Param(c, "uid")
		if uid == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "subscription uid 不能为空"})
			return nil
		}
		profile := deps.Store.Get(uid)
		if profile == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": fmt.Sprintf("subscription_uid=%s 不存在", uid)})
			return nil
		}
		if profile.Provider != "new_api" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "仅 new_api 类型订阅支持删除主账号"})
			return nil
		}
		if strings.TrimSpace(profile.AccessToken) == "" {
			httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": "订阅当前没有主账号"})
			return nil
		}

		// 1) 从关联渠道剔除主账号的 key（复用账号剔除：仅读取 ProvisionedKeys）。
		var removedTokenIDs map[int64]struct{}
		if deps.SyncService != nil {
			removedTokenIDs = deps.SyncService.RemoveAccountKeysFromChannels(profile, NewApiAccount{
				ProvisionedKeys: profile.ProvisionedKeys,
			})
		}

		// 2) best-effort 回收远端 key（凭证仍有效时执行；失败仅记录日志，不阻断删除）。
		if len(removedTokenIDs) > 0 {
			ctx, cancel := context.WithTimeout(httpbridge.Request(c).Context(), 15*time.Second)
			defer cancel()
			adapter := adapterForAccount(deps, profile, NewApiAccount{})
			for tokenID := range removedTokenIDs {
				if err := adapter.DeleteToken(ctx, profile.BaseURL, profile.AccessToken, profile.UserID, profile.AuthTokenMode, int(tokenID)); err != nil {
					log.Printf("[NewApi-Account] 回收远端 key 失败 primary tokenID=%d: %v", tokenID, err)
				}
			}
		}

		// 3) 清空订阅级凭证与主账号信息；订阅本体保留。
		if err := deps.Store.Patch(uid, nil, func(p *SubscriptionProfile) error {
			p.AccessToken, p.UserID, p.Username, p.AuthTokenMode = "", "", "", ""
			p.ProvisionedKeys = nil
			p.ProvisionedTokenID = 0
			p.Balance, p.UsedQuota = 0, 0
			p.LastBalanceRefreshAt = nil
			p.LastBalanceRefreshError = ""
			return nil
		}); err != nil {
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"success": true})
		return nil
	}
}

// handleDeleteSubscriptionAccount 删除指定账号：先从关联渠道剔除该账号的 key，再 best-effort 回收远端 key，
// 最后从订阅移除账号。渠道剔除失败不阻断删除，避免残留远端 key 阻塞账号移除。
func handleDeleteSubscriptionAccount(deps *NewApiRouteDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		uid := httpbridge.Param(c, "uid")
		accountUID := httpbridge.Param(c, "accountUid")
		if uid == "" || accountUID == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "subscription uid 和 account uid 不能为空"})
			return nil
		}

		profile := deps.Store.Get(uid)
		if profile == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": fmt.Sprintf("subscription_uid=%s 不存在", uid)})
			return nil
		}
		var account *NewApiAccount
		for i := range profile.Accounts {
			if profile.Accounts[i].AccountUID == accountUID {
				account = &profile.Accounts[i]
				break
			}
		}
		if account == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": fmt.Sprintf("account_uid=%s 不存在", accountUID)})
			return nil
		}

		// 1) 从关联渠道剔除该账号的 key（返回被移除的 tokenID，供远端回收）。
		var removedTokenIDs map[int64]struct{}
		if deps.SyncService != nil {
			removedTokenIDs = deps.SyncService.RemoveAccountKeysFromChannels(profile, *account)
		}

		// 2) best-effort 回收远端 key。失败仅记录日志，不阻断删除。
		if len(removedTokenIDs) > 0 {
			ctx, cancel := context.WithTimeout(httpbridge.Request(c).Context(), 15*time.Second)
			defer cancel()
			adapter := adapterForAccount(deps, profile, *account)
			for tokenID := range removedTokenIDs {
				if err := adapter.DeleteToken(ctx, profile.BaseURL, account.AccessToken, account.UserID, account.AuthTokenMode, int(tokenID)); err != nil {
					log.Printf("[NewApi-Account] 回收远端 key 失败 account=%s tokenID=%d: %v", accountUID, tokenID, err)
				}
			}
		}

		// 3) 移除账号。
		if err := deps.Store.RemoveAccount(uid, accountUID); err != nil {
			if strings.Contains(err.Error(), "不存在") {
				httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": err.Error()})
				return nil
			}
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"success": true})
		return nil
	}
}

// handleRefreshSubscriptionAccount 刷新单个账号余额。
func handleRefreshSubscriptionAccount(deps *NewApiRouteDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		uid, accountUID := httpbridge.Param(c, "uid"), httpbridge.Param(c, "accountUid")
		if uid == "" || accountUID == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "subscription uid 和 account uid 不能为空"})
			return nil
		}
		profile := deps.Store.Get(uid)
		if profile == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": fmt.Sprintf("subscription_uid=%s 不存在", uid)})
			return nil
		}
		var account NewApiAccount
		found := false
		for _, candidate := range profile.Accounts {
			if candidate.AccountUID == accountUID {
				account, found = candidate, true
				break
			}
		}
		if !found {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": fmt.Sprintf("account_uid=%s 不存在", accountUID)})
			return nil
		}
		ctx, cancel := context.WithTimeout(httpbridge.Request(c).Context(), 15*time.Second)
		defer cancel()
		adapter := adapterForAccount(deps, profile, account)
		balance, _, fetchErr := adapter.FetchBalance(ctx, profile.BaseURL, account.AccessToken, account.UserID, account.AuthTokenMode)
		now := time.Now()
		patchErr := deps.Store.Patch(uid, nil, func(current *SubscriptionProfile) error {
			for i := range current.Accounts {
				if current.Accounts[i].AccountUID != accountUID {
					continue
				}
				current.Accounts[i].LastCheckedAt = now
				if fetchErr != nil {
					current.Accounts[i].Status = "error"
				} else {
					current.Accounts[i].Balance, current.Accounts[i].Status = balance, "active"
				}
				account = current.Accounts[i]
				return nil
			}
			return errAccountMissing
		})
		if patchErr != nil {
			if errors.Is(patchErr, errAccountMissing) || strings.Contains(patchErr.Error(), "不存在") {
				httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": fmt.Sprintf("account_uid=%s 不存在", accountUID)})
				return nil
			}
			if strings.Contains(patchErr.Error(), "version 冲突") {
				httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": patchErr.Error()})
				return nil
			}
			if strings.Contains(patchErr.Error(), "subscription_uid") {
				httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": patchErr.Error()})
				return nil
			}
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": patchErr.Error()})
			return nil
		}
		if fetchErr != nil {
			httpbridge.JSON(c, http.StatusBadGateway, fiber.Map{"error": fmt.Sprintf("刷新余额失败: %v", fetchErr)})
			return nil
		}
		if deps.SyncService != nil {
			_, _ = deps.SyncService.SyncNow(httpbridge.Request(c).Context(), uid)
		}
		httpbridge.JSON(c, http.StatusOK, NewApiAccountItem{AccountUID: account.AccountUID, UserID: account.UserID, DisplayName: account.DisplayName, Balance: account.Balance, Status: account.Status, AccessTokenMasked: maskAccessToken(account.AccessToken), ProvisionedKeys: account.ProvisionedKeys, LastSyncError: account.LastSyncError, LastCheckedAt: account.LastCheckedAt, CreatedAt: account.CreatedAt, ProxyURL: account.ProxyURL, ProxyPreferDirect: account.ProxyPreferDirect})
		return nil
	}
}

// RegisterSubscriptionAccountRoutes 注册 new-api 多账号管理路由。
// NewApiAccountCredentialsUpdateRequest PATCH /accounts/:accountUid/credentials 请求体。
// 代理不在账号级维护（渠道"代理通道"是唯一事实源），故无 proxy 字段。
type NewApiAccountCredentialsUpdateRequest struct {
	AccessToken   *string `json:"accessToken,omitempty"`
	UserID        *string `json:"userId,omitempty"`
	AuthTokenMode *string `json:"authTokenMode,omitempty"`
}

// handleUpdateSubscriptionAccountCredentials 更新子账号凭证，落库前先用新凭证组合
// 调站点验证（对齐主账号 handleUpdateNewApiCredentials 语义），通过后回填余额。
func handleUpdateSubscriptionAccountCredentials(deps *NewApiRouteDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		uid, accountUID := httpbridge.Param(c, "uid"), httpbridge.Param(c, "accountUid")
		if uid == "" || accountUID == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "subscription uid 和 account uid 不能为空"})
			return nil
		}
		profile := deps.Store.Get(uid)
		if profile == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": fmt.Sprintf("subscription_uid=%s 不存在", uid)})
			return nil
		}
		var account *NewApiAccount
		for i := range profile.Accounts {
			if profile.Accounts[i].AccountUID == accountUID {
				account = &profile.Accounts[i]
				break
			}
		}
		if account == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": fmt.Sprintf("account_uid=%s 不存在", accountUID)})
			return nil
		}

		var req NewApiAccountCredentialsUpdateRequest
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "请求参数无效: " + err.Error()})
			return nil
		}
		if req.AccessToken == nil && req.UserID == nil && req.AuthTokenMode == nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "至少需要提交一个凭证字段"})
			return nil
		}

		accessToken := account.AccessToken
		if req.AccessToken != nil {
			accessToken = strings.TrimSpace(*req.AccessToken)
			if accessToken == "" {
				httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "accessToken 不能为空"})
				return nil
			}
		}
		userID := account.UserID
		if req.UserID != nil {
			userID = strings.TrimSpace(*req.UserID)
		}
		authTokenMode := account.AuthTokenMode
		if req.AuthTokenMode != nil {
			authTokenMode = strings.ToLower(strings.TrimSpace(*req.AuthTokenMode))
		}
		if authTokenMode == "" {
			authTokenMode = NewApiAuthModeBearer
		}
		if authTokenMode != NewApiAuthModeBearer && authTokenMode != NewApiAuthModeRaw && authTokenMode != NewApiAuthModeRawAuth {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "authTokenMode 仅支持 bearer 或 raw"})
			return nil
		}

		merged := *account
		merged.AccessToken, merged.UserID, merged.AuthTokenMode = accessToken, userID, authTokenMode
		ctx, cancel := context.WithTimeout(httpbridge.Request(c).Context(), 20*time.Second)
		defer cancel()
		adapter := adapterForAccount(deps, profile, merged)
		self, derivedUserID, err := adapter.VerifyWithFallback(ctx, profile.BaseURL, accessToken, userID, authTokenMode)
		if err != nil {
			httpbridge.JSON(c, http.StatusBadGateway, fiber.Map{"error": fmt.Sprintf("账号验证失败: %v", err)})
			return nil
		}

		now := time.Now()
		updated := merged
		patchErr := deps.Store.Patch(uid, nil, func(current *SubscriptionProfile) error {
			for i := range current.Accounts {
				if current.Accounts[i].AccountUID != accountUID {
					continue
				}
				current.Accounts[i].AccessToken = accessToken
				current.Accounts[i].UserID = derivedUserID
				current.Accounts[i].AuthTokenMode = authTokenMode
				current.Accounts[i].Balance = float64(self.Quota)
				current.Accounts[i].Status = "active"
				current.Accounts[i].LastCheckedAt = now
				updated = current.Accounts[i]
				return nil
			}
			return errAccountMissing
		})
		if patchErr != nil {
			if errors.Is(patchErr, errAccountMissing) || strings.Contains(patchErr.Error(), "不存在") {
				httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": fmt.Sprintf("account_uid=%s 不存在", accountUID)})
				return nil
			}
			if strings.Contains(patchErr.Error(), "version 冲突") {
				httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": patchErr.Error()})
				return nil
			}
			if strings.Contains(patchErr.Error(), "subscription_uid") {
				httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": patchErr.Error()})
				return nil
			}
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": patchErr.Error()})
			return nil
		}

		// 凭证已通过校验；分组/倍率同步失败不回滚凭证，由账号快照 lastSyncError 展示。
		if deps.SyncService != nil {
			_, _ = deps.SyncService.SyncNow(httpbridge.Request(c).Context(), uid)
		}
		httpbridge.JSON(c, http.StatusOK, NewApiAccountItem{
			AccountUID:        updated.AccountUID,
			UserID:            updated.UserID,
			AuthTokenMode:     updated.AuthTokenMode,
			DisplayName:       updated.DisplayName,
			Balance:           updated.Balance,
			Status:            updated.Status,
			AccessTokenMasked: maskAccessToken(updated.AccessToken),
			ProvisionedKeys:   updated.ProvisionedKeys,
			LastSyncError:     updated.LastSyncError,
			LastCheckedAt:     updated.LastCheckedAt,
			CreatedAt:         updated.CreatedAt,
			ProxyURL:          updated.ProxyURL,
			ProxyPreferDirect: updated.ProxyPreferDirect,
		})
		return nil
	}
}

// handleUpdateNewApiCredentials 更新 new-api 主账号凭证，并在落库前验证新凭证组合。
func handleUpdateNewApiCredentials(deps *NewApiRouteDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		uid := strings.TrimSpace(httpbridge.Param(c, "uid"))
		if uid == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "subscription uid 不能为空"})
			return nil
		}

		profile := deps.Store.Get(uid)
		if profile == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": fmt.Sprintf("subscription_uid=%s 不存在", uid)})
			return nil
		}
		if profile.Provider != "new_api" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "仅 new_api 类型订阅支持主账号凭证更新"})
			return nil
		}

		var req NewApiCredentialsUpdateRequest
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "请求参数无效: " + err.Error()})
			return nil
		}
		if req.AccessToken == nil && req.UserID == nil && req.AuthTokenMode == nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "至少需要提交一个凭证字段"})
			return nil
		}

		accessToken := profile.AccessToken
		if req.AccessToken != nil {
			accessToken = strings.TrimSpace(*req.AccessToken)
			if accessToken == "" {
				httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "accessToken 不能为空"})
				return nil
			}
		}
		userID := profile.UserID
		if req.UserID != nil {
			userID = strings.TrimSpace(*req.UserID)
		}
		authTokenMode := profile.AuthTokenMode
		if req.AuthTokenMode != nil {
			authTokenMode = strings.ToLower(strings.TrimSpace(*req.AuthTokenMode))
		}
		if authTokenMode == "" {
			authTokenMode = NewApiAuthModeBearer
		}
		if authTokenMode != NewApiAuthModeBearer && authTokenMode != NewApiAuthModeRaw && authTokenMode != NewApiAuthModeRawAuth {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "authTokenMode 仅支持 bearer 或 raw"})
			return nil
		}

		ctx, cancel := context.WithTimeout(httpbridge.Request(c).Context(), 20*time.Second)
		defer cancel()
		// 代理按生效值验证：请求显式提交优先，否则沿用订阅现有设置
		proxyURL := profile.ProxyURL
		if req.ProxyURL != nil {
			proxyURL = strings.TrimSpace(*req.ProxyURL)
		}
		proxyPreferDirect := profile.ProxyPreferDirect
		if req.ProxyPreferDirect != nil {
			proxyPreferDirect = *req.ProxyPreferDirect
		}
		adapter := NewApiAdapterForProxy(proxyURL, proxyPreferDirect)
		self, derivedUserID, err := adapter.VerifyWithFallback(ctx, profile.BaseURL, accessToken, userID, authTokenMode)
		if err != nil {
			httpbridge.JSON(c, http.StatusBadGateway, fiber.Map{"error": fmt.Sprintf("主账号验证失败: %v", err)})
			return nil
		}

		now := time.Now()
		if err := deps.Store.Patch(uid, req.ExpectedVersion, func(current *SubscriptionProfile) error {
			current.AccessToken = accessToken
			current.UserID = derivedUserID
			current.AuthTokenMode = authTokenMode
			if req.ProxyURL != nil {
				current.ProxyURL = strings.TrimSpace(*req.ProxyURL)
			}
			if req.ProxyPreferDirect != nil {
				current.ProxyPreferDirect = *req.ProxyPreferDirect
			}
			current.Balance = float64(self.Quota)
			current.UsedQuota = self.UsedQuota
			current.LastBalanceRefreshAt = &now
			current.LastBalanceRefreshError = ""
			return nil
		}); err != nil {
			if strings.Contains(err.Error(), "version 冲突") {
				httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": err.Error()})
				return nil
			}
			if strings.Contains(err.Error(), "subscription_uid") {
				httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": err.Error()})
				return nil
			}
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": err.Error()})
			return nil
		}

		// 凭证已通过校验；同步分组/模型与渠道倍率失败时保留凭证，并由订阅快照展示错误供重试。
		if deps.SyncService != nil {
			_, _ = deps.SyncService.SyncNow(httpbridge.Request(c).Context(), uid)
		}
		httpbridge.JSON(c, http.StatusOK, toSubscriptionItem(deps.Store.Get(uid)))
		return nil
	}
}

func RegisterSubscriptionAccountRoutes(router fiber.Router, deps *NewApiRouteDeps) {
	if deps == nil || deps.Store == nil {
		return
	}
	router.Patch("/subscriptions/:uid/newapi-credentials", handleUpdateNewApiCredentials(deps))
	group := router.Group("/subscriptions/:uid/accounts")
	group.Post("", handleAddSubscriptionAccount(deps))
	group.Get("", handleListSubscriptionAccounts(deps))
	group.Delete("/primary", handleDeletePrimaryAccount(deps))
	group.Delete("/:accountUid", handleDeleteSubscriptionAccount(deps))
	group.Post("/:accountUid/refresh", handleRefreshSubscriptionAccount(deps))
	group.Patch("/:accountUid/credentials", handleUpdateSubscriptionAccountCredentials(deps))
}
