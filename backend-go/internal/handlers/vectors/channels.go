// Package vectors provides channel management for the OpenAI-compatible embeddings API.
package vectors

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	handlers "github.com/soulteary/yunqi/internal/handlers"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/httpclient"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/utils"
)

// endpointPathPattern matches a trailing version segment such as "/v1" or
// "/v1beta" so buildEndpointURL can skip appending another version prefix.
var endpointPathPattern = regexp.MustCompile(`/v\d+[a-z]*$`)

func GetUpstreams(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		cfg := cfgManager.GetConfig()
		upstreams := make([]fiber.Map, len(cfg.VectorsUpstream))
		for i, up := range cfg.VectorsUpstream {
			upstreams[i] = common.BuildChannelView(up, i)
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"channels": upstreams})
		return nil
	}
}

func AddUpstream(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		var req struct {
			config.UpstreamConfig
			Placement string `json:"placement"` // 故障转移位置：front（首位）| back（末尾，默认）
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		if err := cfgManager.AddVectorsUpstreamAudited(managementAuditContext, req.UpstreamConfig, req.Placement); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, config.ErrUnsupportedServiceType) || errors.Is(err, config.ErrInvalidEmbeddingCapability) {
				status = http.StatusBadRequest
			} else if errors.Is(err, config.ErrDuplicateChannelName) {
				status = http.StatusConflict
			}
			httpbridge.JSON(c, status, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"message": "Vectors upstream added successfully"})
		return nil
	}
}

func UpdateUpstream(cfgManager *config.ConfigManager, sch *scheduler.ChannelScheduler) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		id, err := strconv.Atoi(httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid upstream ID"})
			return nil
		}

		var updates config.UpstreamUpdate
		if err := httpbridge.BindJSON(c, &updates); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}

		oldName := ""
		if updates.Name != nil {
			cfg := cfgManager.GetConfig()
			if id >= 0 && id < len(cfg.VectorsUpstream) {
				oldName = cfg.VectorsUpstream[id].Name
			}
		}

		shouldResetMetrics, err := cfgManager.UpdateVectorsUpstreamAudited(managementAuditContext, id, updates)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, config.ErrDuplicateChannelName) {
				status = http.StatusConflict
			} else if errors.Is(err, config.ErrUnsupportedServiceType) || errors.Is(err, config.ErrInvalidEmbeddingCapability) || strings.Contains(err.Error(), "无效") {
				status = http.StatusBadRequest
			}
			httpbridge.JSON(c, status, fiber.Map{"error": err.Error()})
			return nil
		}

		if updates.Name != nil && oldName != "" && oldName != *updates.Name {
			if logStore := sch.GetChannelLogStore(scheduler.ChannelKindVectors); logStore != nil {
				logStore.RenameChannel(oldName, *updates.Name)
			}
		}
		if shouldResetMetrics {
			sch.ResetChannelMetrics(id, scheduler.ChannelKindVectors)
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"message": "Vectors upstream updated successfully"})
		return nil
	}
}

func DeleteUpstream(cfgManager *config.ConfigManager, channelScheduler *scheduler.ChannelScheduler) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		id, err := strconv.Atoi(httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid upstream ID"})
			return nil
		}

		removed, err := cfgManager.RemoveVectorsUpstreamAudited(managementAuditContext, id)
		if err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "无效") {
				status = http.StatusNotFound
			}
			httpbridge.JSON(c, status, fiber.Map{"error": err.Error()})
			return nil
		}

		channelScheduler.DeleteChannelLogs(removed, scheduler.ChannelKindVectors)
		channelScheduler.DeleteChannelMetrics(removed, scheduler.ChannelKindVectors)
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"message": "Vectors upstream deleted successfully"})
		return nil
	}
}

func AddApiKey(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		id, err := strconv.Atoi(httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid upstream ID"})
			return nil
		}

		var req struct {
			APIKey string `json:"apiKey"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid request body"})
			return nil
		}

		if err := cfgManager.AddVectorsAPIKeyAudited(managementAuditContext, id, req.APIKey); err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "无效") {
				status = http.StatusNotFound
			} else if strings.Contains(err.Error(), "已存在") {
				status = http.StatusBadRequest
			}
			httpbridge.JSON(c, status, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"message": "API key added", "success": true})
		return nil
	}
}

func DeleteApiKey(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		id, err := strconv.Atoi(httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid upstream ID"})
			return nil
		}
		apiKey := httpbridge.Param(c, "apiKey")
		if apiKey == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "API key is required"})
			return nil
		}

		if err := cfgManager.RemoveVectorsAPIKeyAudited(managementAuditContext, id, apiKey); err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "无效") || strings.Contains(err.Error(), "不存在") {
				status = http.StatusNotFound
			}
			httpbridge.JSON(c, status, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"message": "API key deleted"})
		return nil
	}
}

func MoveApiKeyToTop(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		id, err := strconv.Atoi(httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid upstream ID"})
			return nil
		}
		if err := cfgManager.MoveVectorsAPIKeyToTopAudited(managementAuditContext, id, httpbridge.Param(c, "apiKey")); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"message": "API key moved to top"})
		return nil
	}
}

func MoveApiKeyToBottom(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		id, err := strconv.Atoi(httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid upstream ID"})
			return nil
		}
		if err := cfgManager.MoveVectorsAPIKeyToBottomAudited(managementAuditContext, id, httpbridge.Param(c, "apiKey")); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"message": "API key moved to bottom"})
		return nil
	}
}

func ReorderChannels(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		var req struct {
			Order      []int `json:"order"`
			Priorities []int `json:"priorities"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid request body"})
			return nil
		}
		if err := cfgManager.ReorderVectorsUpstreamsAudited(managementAuditContext, req.Order, req.Priorities); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"success": true, "message": "Vectors channel priority updated"})
		return nil
	}
}

func SetChannelStatus(cfgManager *config.ConfigManager) fiber.Handler {
	adapter := handlers.AuditedChannelStatusConfigManagerFunc(func(audit config.ManagementAuditContext, index int, status string) error {
		return cfgManager.SetVectorsChannelStatusAudited(audit, index, status)
	})
	return handlers.NamedChannelStatusHandler(adapter, "Vectors channel status updated")
}

func SetChannelPromotion(cfgManager *config.ConfigManager) fiber.Handler {
	adapter := handlers.AuditedPromotionConfigManagerFunc(func(audit config.ManagementAuditContext, index int, duration time.Duration) error {
		return cfgManager.SetVectorsChannelPromotionAudited(audit, index, duration)
	})
	return handlers.NamedChannelPromotionHandler(adapter, "Invalid channel ID", "Invalid request body", "Vectors promotion cleared", "Vectors promotion set")
}

func PingChannel(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := strconv.Atoi(httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid channel ID"})
			return nil
		}
		cfg := cfgManager.GetConfig()
		if id < 0 || id >= len(cfg.VectorsUpstream) {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "Channel not found"})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, common.PingSingleBaseURLUpstream(cfg.VectorsUpstream[id], buildPingRequest))
		return nil
	}
}

func PingAllChannels(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		cfg := cfgManager.GetConfig()
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"channels": common.PingAllSingleBaseURLUpstreams(cfg.VectorsUpstream, buildPingRequest, true)["channels"]})
		return nil
	}
}

func buildPingRequest(upstream config.UpstreamConfig, baseURL string) (*http.Request, error) {
	req, _ := http.NewRequest(http.MethodGet, buildModelsURL(baseURL), nil)
	if len(upstream.APIKeys) > 0 {
		utils.SetAuthenticationHeaderWithOverride(req.Header, upstream.APIKeys[0], upstream.AuthHeader)
	}
	req.Header.Set("Content-Type", "application/json")
	utils.ApplyCustomHeaders(req.Header, upstream.CustomHeaders)
	return req, nil
}

func buildEndpointURL(baseURL, versionPrefix, endpoint string) string {
	trimmed := strings.TrimSpace(baseURL)
	skipVersionPrefix := strings.HasSuffix(trimmed, "#")
	if skipVersionPrefix {
		trimmed = strings.TrimSuffix(trimmed, "#")
	}
	trimmed = strings.TrimRight(trimmed, "/")

	if !skipVersionPrefix && !endpointPathPattern.MatchString(trimmed) {
		trimmed += versionPrefix
	}
	return trimmed + endpoint
}

func buildModelsURL(baseURL string) string {
	return buildEndpointURL(baseURL, "/v1", "/models")
}

type GetModelsRequest struct {
	Key      string   `json:"key"`
	BaseURL  string   `json:"baseUrl"`
	BaseURLs []string `json:"baseUrls"`
	ProxyURL string   `json:"proxyUrl"`
	// ProxyPreferDirect 直连优先（nil=未指定，回退渠道配置）；仅在配置了代理时有意义
	ProxyPreferDirect  *bool                       `json:"proxyPreferDirect"`
	OutboundNetwork    config.OutboundNetworkPatch `json:"outboundNetwork"`
	InsecureSkipVerify *bool                       `json:"insecureSkipVerify"`
	CustomHeaders      map[string]string           `json:"customHeaders"`
	AuthHeader         string                      `json:"authHeader"`
	// LearnedClientFingerprint 上游存在客户端指纹校验（保活 L1 等无渠道上下文的
	// 调用方传入渠道学习到的标记），命中时请求带 Claude Code 客户端伪装头。
	LearnedClientFingerprint bool `json:"learnedClientFingerprint,omitempty"`
}

func GetChannelModels(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := strconv.Atoi(httpbridge.Param(c, "id"))
		if err != nil {
			if common.RespondNetworkEgressError(c, err) {
				return nil
			}
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid channel ID"})
			return nil
		}

		var req GetModelsRequest
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid request body"})
			return nil
		}

		var baseURL string
		channelName := "temporary channel"
		insecureSkipVerify := false
		proxyURL := req.ProxyURL
		proxyPreferDirect := req.ProxyPreferDirect != nil && *req.ProxyPreferDirect
		outboundNetwork := req.OutboundNetwork.Value
		authHeader := req.AuthHeader
		learnedClientFingerprint := req.LearnedClientFingerprint

		if strings.TrimSpace(req.BaseURL) != "" {
			if err := utils.ValidateBaseURL(req.BaseURL); err != nil {
				log.Printf("[Vectors-Models] SSRF guard blocked baseUrl: caller=%s error=%v", httpbridge.ClientIP(c), err)
				httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": fmt.Sprintf("Invalid baseUrl: %v", err)})
				return nil
			}
			baseURL = req.BaseURL
			if req.InsecureSkipVerify != nil {
				insecureSkipVerify = *req.InsecureSkipVerify
			}
			log.Printf("[Vectors-Models] using temporary baseUrl: caller=%s", httpbridge.ClientIP(c))
		} else {
			cfg := cfgManager.GetConfig()
			if id < 0 || id >= len(cfg.VectorsUpstream) {
				httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "Channel not found"})
				return nil
			}
			channel := cfg.VectorsUpstream[id]
			baseURL = channel.BaseURL
			channelName = channel.Name
			insecureSkipVerify = channel.InsecureSkipVerify
			proxyURL = channel.ProxyURL
			proxyPreferDirect = channel.ProxyPreferDirect
			outboundNetwork = channel.OutboundNetwork
			if req.OutboundNetwork.Set {
				outboundNetwork = req.OutboundNetwork.Value
			}
			authHeader = channel.AuthHeader
			learnedClientFingerprint = channel.LearnedClientFingerprint || req.LearnedClientFingerprint
			if req.InsecureSkipVerify != nil {
				insecureSkipVerify = *req.InsecureSkipVerify
			}
			if req.ProxyURL != "" {
				proxyURL = req.ProxyURL
			}
			if req.ProxyPreferDirect != nil {
				proxyPreferDirect = *req.ProxyPreferDirect
			}
			if req.AuthHeader != "" {
				authHeader = req.AuthHeader
			}
		}

		apiKey := req.Key
		if apiKey == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "No API key provided"})
			return nil
		}

		url := buildModelsURL(baseURL)
		if req.OutboundNetwork.Set {
			outboundNetwork = req.OutboundNetwork.Value
		}
		clientOpts := httpclient.ClientOptions{
			OutboundNetwork:   outboundNetwork,
			Timeout:           10 * time.Second,
			Insecure:          insecureSkipVerify,
			ProxyURL:          proxyURL,
			ProxyPreferDirect: proxyPreferDirect,
		}
		client := httpclient.GetManager().GetClient(clientOpts)
		if req.BaseURL != "" {
			// 新增模式下的临时代理参数高变，不进缓存
			client = httpclient.GetManager().NewClient(clientOpts)
		}

		httpReq, err := http.NewRequestWithContext(httpbridge.Request(c).Context(), http.MethodGet, url, nil)
		if err != nil {
			if common.RespondNetworkEgressError(c, err) {
				return nil
			}
			log.Printf("[Vectors-Models] create request failed: channel=%s, error=%s", channelName, sanitizeModelsProbeError(err, url))
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": fmt.Sprintf("Failed to create request: %v", err)})
			return nil
		}
		utils.SetAuthenticationHeaderWithOverride(httpReq.Header, apiKey, authHeader)
		// 已学习客户端伪装标记的上游：请求带 Claude Code 探针头，
		// 避免被上游客户端指纹校验拒绝。
		if learnedClientFingerprint {
			utils.ApplyClaudeCodeProbeHeaders(httpReq.Header, "")
		}
		httpReq.Header.Set("Content-Type", "application/json")
		utils.ApplyCustomHeaders(httpReq.Header, req.CustomHeaders)

		log.Printf("[Vectors-Models] requesting models: channel=%s, key=%s", channelName, utils.MaskAPIKey(apiKey))
		resp, err := client.Do(httpReq)
		if err != nil {
			if common.RespondNetworkEgressError(c, err) {
				return nil
			}
			log.Printf("[Vectors-Models] request failed: channel=%s, key=%s, error=%s", channelName, utils.MaskAPIKey(apiKey), sanitizeModelsProbeError(err, url))
			httpbridge.JSON(c, http.StatusBadGateway, fiber.Map{"error": fmt.Sprintf("Failed to fetch models: %v", err)})
			return nil
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			if common.RespondNetworkEgressError(c, err) {
				return nil
			}
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": fmt.Sprintf("Failed to read response: %v", err)})
			return nil
		}
		if resp.StatusCode == http.StatusUnauthorized {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Upstream API key is invalid", "statusCode": http.StatusUnauthorized, "details": string(body)})
			return nil
		}
		if resp.StatusCode == http.StatusOK {
			// 透传上游响应体时注入真实状态码，供前端展示（不再硬编码 200）。
			body = common.InjectUpstreamStatusCode(body, resp.StatusCode)
		}
		httpbridge.Data(c, resp.StatusCode, "application/json", body)
		return nil
	}
}

func sanitizeModelsProbeError(err error, requestURL string) string {
	if err == nil {
		return ""
	}
	var urlErr *neturl.Error
	if errors.As(err, &urlErr) {
		if inner := sanitizeDiagnosticError(urlErr.Err); inner != "" {
			return urlErr.Op + ": " + inner
		}
		return urlErr.Op
	}
	msg := sanitizeDiagnosticError(err)
	if requestURL != "" {
		msg = strings.ReplaceAll(msg, requestURL, "[redacted-url]")
	}
	return msg
}
