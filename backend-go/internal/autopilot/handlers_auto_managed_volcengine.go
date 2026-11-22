package autopilot

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/utils"
)

func handleSetVolcengineAccessKey(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		var req struct {
			AccessKeyID     string `json:"accessKeyId"`
			SecretAccessKey string `json:"secretAccessKey"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "无效的请求体"})
			return nil
		}
		accountUID := strings.TrimSpace(httpbridge.Param(c, "accountUid"))
		credentialUID := strings.TrimSpace(httpbridge.Param(c, "credentialUid"))
		channels := deps.CfgManager.GetAccountChannels(accountUID)
		if len(channels) == 0 || channels[0].Upstream.ProviderID != "volcengine" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "仅火山方舟自动托管账号支持绑定 Access Key"})
			return nil
		}
		credential, ok := deps.CfgManager.GetManagedAccountCredential(accountUID, credentialUID)
		if !ok {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "推理 Key 凭证不存在"})
			return nil
		}
		pair := &config.VolcengineAccessKeyPair{AccessKeyID: strings.TrimSpace(req.AccessKeyID), SecretAccessKey: strings.TrimSpace(req.SecretAccessKey)}
		planClient := &volcenginePlanClient{}
		if deps.Runner != nil {
			planClient.Endpoint = deps.Runner.volcengineControlPlaneEndpoint
			planClient.HTTPClient = deps.Runner.client
		}
		hint := ""
		for _, channel := range channels {
			for _, keyConfig := range channel.Upstream.APIKeyConfigs {
				if keyConfig.CredentialUID == credentialUID {
					hint = volcenginePlanFromBaseURL(keyConfig.BaseURL)
					break
				}
			}
			if hint != "" {
				break
			}
		}
		buckets, plan, err := planClient.DetectPlans(httpbridge.Request(c).Context(), pair, hint)
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		if err := deps.CfgManager.SetManagedAccountVolcengineAccessKeyAudited(managementAuditContext, accountUID, credentialUID, req.AccessKeyID, req.SecretAccessKey); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		if err := deps.CfgManager.SetManagedAccountVolcenginePlanAudited(managementAuditContext, accountUID, credentialUID, plan.Plan, plan.Tier, plan.Status); err != nil {
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": err.Error()})
			return nil
		}
		// 逐桶查询用量：单桶失败只记桶级快照，不阻断保存流程。
		buckets = planClient.FetchBucketsUsage(httpbridge.Request(c).Context(), pair, buckets)
		usage := primaryUsageFromBuckets(buckets, plan.Plan)
		if usage == nil {
			usage = &config.VolcenginePlanUsage{FetchedAt: time.Now(), Error: "未查询到主套餐用量"}
		}
		if usage.Error != "" {
			log.Printf("[Volcengine-Usage] 查询套餐用量失败: account=%s credential=%s err=%s", accountUID, credentialUID, usage.Error)
		}
		if err := deps.CfgManager.SetManagedAccountVolcenginePlanUsageAudited(managementAuditContext, accountUID, credentialUID, usage); err != nil {
			log.Printf("[Volcengine-Usage] 保存套餐用量失败: %v", err)
		}
		if err := deps.CfgManager.SetManagedAccountVolcenginePlanBucketsAudited(managementAuditContext, accountUID, credentialUID, buckets); err != nil {
			log.Printf("[Volcengine-Usage] 保存套餐桶快照失败: %v", err)
		}
		if usage.Error == "" {
			config.TryRestoreDisabledKeysByUsage(deps.CfgManager, accountUID, credential.APIKey, credentialUID)
		}
		started := 0
		for _, channel := range deps.CfgManager.GetAccountChannels(accountUID) {
			if triggerDiscoveryForChannel(deps, channel.Kind, channel.Upstream.ChannelUID) {
				started++
			}
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"accountUid": accountUID, "credentialUid": credentialUID,
			"accessKeyIdMask": utils.MaskAPIKey(strings.TrimSpace(req.AccessKeyID)),
			"plan":            plan.Plan, "planTier": plan.Tier, "planStatus": plan.Status,
			"usage":            usage,
			"plans":            buckets,
			"discoveryStarted": started,
		})
		return nil
	}
}

func handleClearVolcengineAccessKey(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		accountUID := strings.TrimSpace(httpbridge.Param(c, "accountUid"))
		credentialUID := strings.TrimSpace(httpbridge.Param(c, "credentialUid"))
		if err := deps.CfgManager.ClearManagedAccountVolcengineAccessKeyAudited(managementAuditContext, accountUID, credentialUID); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.Status(c, http.StatusNoContent)
		return nil
	}
}

const volcenginePlanUsageTTL = 5 * time.Minute

func handleRefreshVolcenginePlanUsage(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if deps == nil || deps.CfgManager == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "配置管理器不可用"})
			return nil
		}
		accountUID := strings.TrimSpace(httpbridge.Param(c, "accountUid"))
		credentialUID := strings.TrimSpace(httpbridge.Param(c, "credentialUid"))
		credential, ok := deps.CfgManager.GetManagedAccountCredential(accountUID, credentialUID)
		if !ok || credential.VolcengineAccessKey == nil {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "未绑定火山 Access Key"})
			return nil
		}
		accessKey := credential.VolcengineAccessKey
		// TTL 缓存：距上次成功查询未超过 TTL 时直接返回缓存。
		force := strings.EqualFold(strings.TrimSpace(httpbridge.Query(c, "force")), "true")
		if !force && accessKey.Usage != nil && accessKey.Usage.Error == "" &&
			time.Since(accessKey.Usage.FetchedAt) < volcenginePlanUsageTTL {
			httpbridge.JSON(c, http.StatusOK, fiber.Map{"usage": accessKey.Usage, "cached": true})
			return nil
		}
		pair := &config.VolcengineAccessKeyPair{AccessKeyID: accessKey.AccessKeyID, SecretAccessKey: accessKey.SecretAccessKey}
		planClient := &volcenginePlanClient{}
		if deps.Runner != nil {
			planClient.Endpoint = deps.Runner.volcengineControlPlaneEndpoint
			planClient.HTTPClient = deps.Runner.client
		}
		// 多桶快照存在时逐桶刷新（单桶失败只记桶级快照）；旧数据或主桶缺失退回单桶查询。
		var buckets []config.VolcenginePlanBucket
		if len(accessKey.Plans) > 0 {
			buckets = planClient.FetchBucketsUsage(httpbridge.Request(c).Context(), pair, accessKey.Plans)
		}
		usage := primaryUsageFromBuckets(buckets, accessKey.Plan)
		if usage == nil {
			u, err := planClient.FetchUsage(httpbridge.Request(c).Context(), pair, accessKey.Plan)
			if err != nil {
				u = &config.VolcenginePlanUsage{FetchedAt: time.Now(), Error: err.Error()}
				log.Printf("[Volcengine-Usage] 查询套餐用量失败: account=%s credential=%s err=%v", accountUID, credentialUID, err)
			}
			usage = u
		}
		if saveErr := deps.CfgManager.SetManagedAccountVolcenginePlanUsageAudited(managementAuditContext, accountUID, credentialUID, usage); saveErr != nil {
			log.Printf("[Volcengine-Usage] 保存套餐用量失败: %v", saveErr)
		}
		if len(buckets) > 0 {
			if saveErr := deps.CfgManager.SetManagedAccountVolcenginePlanBucketsAudited(managementAuditContext, accountUID, credentialUID, buckets); saveErr != nil {
				log.Printf("[Volcengine-Usage] 保存套餐桶快照失败: %v", saveErr)
			}
		}
		if usage.Error == "" {
			config.TryRestoreDisabledKeysByUsage(deps.CfgManager, accountUID, credential.APIKey, credentialUID)
		}
		if usage.Error != "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": usage.Error, "usage": usage})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"usage": usage, "cached": false, "plans": buckets})
		return nil
	}
}
