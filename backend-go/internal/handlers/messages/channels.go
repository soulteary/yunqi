// Package messages 提供 Claude Messages API 的渠道管理
package messages

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/autopilot"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/copilot"
	handlers "github.com/soulteary/yunqi/internal/handlers"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/httpclient"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/utils"
)

// GetUpstreams 获取上游列表 (兼容前端 channels 字段名)
func GetUpstreams(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		cfg := cfgManager.GetConfig()

		upstreams := make([]fiber.Map, len(cfg.Upstream))
		for i, up := range cfg.Upstream {
			upstreams[i] = common.BuildChannelView(up, i)
		}
		httpbridge.JSON(c, 200, fiber.Map{
			"channels": upstreams,
		})
		return nil
	}
}

// AddUpstream 添加上游
func AddUpstream(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req struct {
			config.UpstreamConfig
			Placement string `json:"placement"` // 故障转移位置：front（首位）| back（末尾，默认）
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid request body"})
			return nil
		}

		if err := cfgManager.AddUpstream(req.UpstreamConfig, req.Placement); err != nil {
			httpbridge.JSON(c, 500, fiber.Map{"error": "Failed to save config"})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{
			"message":  "上游已添加",
			"upstream": req.UpstreamConfig,
		})
		return nil
	}
}

// UpdateUpstream 更新上游
func UpdateUpstream(cfgManager *config.ConfigManager, sch *scheduler.ChannelScheduler) fiber.Handler {
	return func(c fiber.Ctx) error {
		idStr := httpbridge.Param(c, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid upstream ID"})
			return nil
		}

		var updates config.UpstreamUpdate
		if err := httpbridge.BindJSON(c, &updates); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid request body"})
			return nil
		}

		oldName := ""
		{
			cfg := cfgManager.GetConfig()
			if id >= 0 && id < len(cfg.Upstream) {
				oldName = cfg.Upstream[id].Name
			}
		}

		shouldResetMetrics, err := cfgManager.UpdateUpstream(id, updates)
		if err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				httpbridge.JSON(c, 404, fiber.Map{"error": "Upstream not found"})
			} else {
				httpbridge.JSON(c, 500, fiber.Map{"error": "Failed to save config"})
			}
			return nil
		}

		// 名称由后端按首个 baseURL 派生：baseURL 顺序或首地址变化可能触发改名，需迁移日志归属
		newName := ""
		{
			cfg := cfgManager.GetConfig()
			if id >= 0 && id < len(cfg.Upstream) {
				newName = cfg.Upstream[id].Name
			}
		}
		if oldName != "" && newName != "" && oldName != newName {
			if logStore := sch.GetChannelLogStore(scheduler.ChannelKindMessages); logStore != nil {
				logStore.RenameChannel(oldName, newName)
			}
		}

		if shouldResetMetrics {
			sch.ResetChannelMetrics(id, scheduler.ChannelKindMessages)
		}

		cfg := cfgManager.GetConfig()
		httpbridge.JSON(c, 200, fiber.Map{
			"message":  "上游已更新",
			"upstream": cfg.Upstream[id],
		})
		return nil
	}
}

// DeleteUpstream 删除上游
func DeleteUpstream(cfgManager *config.ConfigManager, channelScheduler *scheduler.ChannelScheduler) fiber.Handler {
	return func(c fiber.Ctx) error {
		idStr := httpbridge.Param(c, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid upstream ID"})
			return nil
		}

		removed, err := cfgManager.RemoveUpstream(id)
		if err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				httpbridge.JSON(c, 404, fiber.Map{"error": "Upstream not found"})
			} else {
				httpbridge.JSON(c, 500, fiber.Map{"error": "Failed to save config"})
			}
			return nil
		}

		channelScheduler.DeleteChannelLogs(removed, scheduler.ChannelKindMessages)
		channelScheduler.DeleteChannelMetrics(removed, scheduler.ChannelKindMessages)
		httpbridge.JSON(c, 200, fiber.Map{
			"message": "上游已删除",
			"removed": removed,
		})
		return nil
	}
}

// AddApiKey 添加 API 密钥
func AddApiKey(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		idStr := httpbridge.Param(c, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid upstream ID"})
			return nil
		}

		var req struct {
			APIKey string `json:"apiKey"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid request body"})
			return nil
		}

		if err := cfgManager.AddAPIKey(id, req.APIKey); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				httpbridge.JSON(c, 404, fiber.Map{"error": "Upstream not found"})
			} else if strings.Contains(err.Error(), "API密钥已存在") {
				httpbridge.JSON(c, 400, fiber.Map{"error": "API密钥已存在"})
			} else {
				httpbridge.JSON(c, 500, fiber.Map{"error": "Failed to save config"})
			}
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{
			"message": "API密钥已添加",
			"success": true,
		})
		return nil
	}
}

// DeleteApiKey 删除 API 密钥
func DeleteApiKey(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		idStr := httpbridge.Param(c, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid upstream ID"})
			return nil
		}

		apiKey := httpbridge.Param(c, "apiKey")
		if apiKey == "" {
			httpbridge.JSON(c, 400, fiber.Map{"error": "API key is required"})
			return nil
		}

		if err := cfgManager.RemoveAPIKey(id, apiKey); err != nil {
			if strings.Contains(err.Error(), "无效的上游索引") {
				httpbridge.JSON(c, 404, fiber.Map{"error": "Upstream not found"})
			} else if strings.Contains(err.Error(), "API密钥不存在") {
				httpbridge.JSON(c, 404, fiber.Map{"error": "API key not found"})
			} else {
				httpbridge.JSON(c, 500, fiber.Map{"error": "Failed to save config"})
			}
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{
			"message": "API密钥已删除",
		})
		return nil
	}
}

// MoveApiKeyToTop 将 API 密钥移到顶部
func MoveApiKeyToTop(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		idStr := httpbridge.Param(c, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid upstream ID"})
			return nil
		}

		apiKey := httpbridge.Param(c, "apiKey")
		if apiKey == "" {
			httpbridge.JSON(c, 400, fiber.Map{"error": "API key is required"})
			return nil
		}

		if err := cfgManager.MoveAPIKeyToTop(id, apiKey); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"message": "API密钥已移到顶部"})
		return nil
	}
}

// MoveApiKeyToBottom 将 API 密钥移到底部
func MoveApiKeyToBottom(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		idStr := httpbridge.Param(c, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid upstream ID"})
			return nil
		}

		apiKey := httpbridge.Param(c, "apiKey")
		if apiKey == "" {
			httpbridge.JSON(c, 400, fiber.Map{"error": "API key is required"})
			return nil
		}

		if err := cfgManager.MoveAPIKeyToBottom(id, apiKey); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"message": "API密钥已移到底部"})
		return nil
	}
}

// ReorderChannels 重新排序渠道
func ReorderChannels(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req struct {
			Order      []int `json:"order"`
			Priorities []int `json:"priorities"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid request body"})
			return nil
		}

		if err := cfgManager.ReorderUpstreams(req.Order, req.Priorities); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"message": "渠道顺序已更新"})
		return nil
	}
}

// SetChannelStatus 设置渠道状态
func SetChannelStatus(cfgManager *config.ConfigManager) fiber.Handler {
	return handlers.NamedChannelStatusHandler(cfgManager, "渠道状态已更新")
}

// SetChannelPromotion 设置渠道促销期
// 促销期内的渠道会被优先选择，忽略 trace 亲和性
func SetChannelPromotion(cfgManager *config.ConfigManager) fiber.Handler {
	return handlers.SetChannelPromotion(cfgManager)
}

// PingChannel Ping单个渠道
func PingChannel(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		idStr := httpbridge.Param(c, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid channel ID"})
			return nil
		}

		cfg := cfgManager.GetConfig()
		if id < 0 || id >= len(cfg.Upstream) {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "Channel not found"})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, common.PingMultiBaseURLUpstream(cfg.Upstream[id], buildPingRequest))
		return nil
	}
}

// PingAllChannels Ping所有渠道
func PingAllChannels(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		cfg := cfgManager.GetConfig()
		httpbridge.JSON(c, http.StatusOK, common.PingAllMultiBaseURLUpstreams(cfg.Upstream, buildPingRequest))
		return nil
	}
}

func buildPingRequest(_ config.UpstreamConfig, baseURL string) (*http.Request, error) {
	return http.NewRequest(http.MethodHead, baseURL, nil)
}

// GetModelsRequest 获取模型列表的请求体
type GetModelsRequest struct {
	Key         string   `json:"key"`
	BaseURL     string   `json:"baseUrl"`
	BaseURLs    []string `json:"baseUrls"`
	ServiceType string   `json:"serviceType"`
	ProxyURL    string   `json:"proxyUrl"`
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

// GetChannelModels 获取指定渠道的模型列表（支持临时 Key）
func GetChannelModels(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		// 1. 解析渠道 ID
		idStr := httpbridge.Param(c, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			if common.RespondNetworkEgressError(c, err) {
				return nil
			}
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid channel ID"})
			return nil
		}

		// 2. 从请求体读取参数
		var req GetModelsRequest
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid request body"})
			return nil
		}

		// 3. 获取 baseUrl（优先使用请求体中的临时 baseUrl，用于新增渠道场景）
		var baseURL string
		var channelName string
		var channel *config.UpstreamConfig
		var serviceType string
		var insecureSkipVerify bool
		var proxyURL string
		var proxyPreferDirect bool
		var outboundNetwork = req.OutboundNetwork.Value
		var authHeader string
		var learnedClientFingerprint bool

		if req.BaseURL != "" {
			// 新增模式：使用临时 baseUrl
			// SSRF 防护：验证用户提供的 baseURL
			if err := utils.ValidateBaseURL(req.BaseURL); err != nil {
				log.Printf("[Messages-Models] SSRF 防护拦截: %v", err)
				httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": fmt.Sprintf("无效的 baseUrl: %v", err)})
				return nil
			}
			baseURL = req.BaseURL
			channelName = "临时渠道"
			serviceType = req.ServiceType
			insecureSkipVerify = false
			proxyURL = ""
			if req.InsecureSkipVerify != nil {
				insecureSkipVerify = *req.InsecureSkipVerify
			}
			if req.ProxyURL != "" {
				proxyURL = req.ProxyURL
			}
			if req.ProxyPreferDirect != nil {
				proxyPreferDirect = *req.ProxyPreferDirect
			}
			authHeader = req.AuthHeader
			learnedClientFingerprint = req.LearnedClientFingerprint
			log.Printf("[Messages-Models] 使用临时 baseUrl: %s", baseURL)
		} else {
			// 编辑模式：从配置中读取渠道信息
			cfg := cfgManager.GetConfig()
			if id < 0 || id >= len(cfg.Upstream) {
				httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "Channel not found"})
				return nil
			}

			channel = &cfg.Upstream[id]
			baseURL = channel.BaseURL
			channelName = channel.Name
			serviceType = channel.ServiceType
			insecureSkipVerify = channel.InsecureSkipVerify
			proxyURL = channel.ProxyURL
			proxyPreferDirect = channel.ProxyPreferDirect
			outboundNetwork = channel.OutboundNetwork
			if req.OutboundNetwork.Set {
				outboundNetwork = req.OutboundNetwork.Value
			}
			authHeader = channel.AuthHeader
			learnedClientFingerprint = channel.LearnedClientFingerprint || req.LearnedClientFingerprint
			if req.BaseURL != "" {
				if err := utils.ValidateBaseURL(req.BaseURL); err != nil {
					log.Printf("[Messages-Models] SSRF 防护拦截: %v", err)
					httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": fmt.Sprintf("无效的 baseUrl: %v", err)})
					return nil
				}
				baseURL = req.BaseURL
			}
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
			if req.ServiceType != "" {
				serviceType = req.ServiceType
			}
		}

		// 4. 验证 API Key
		apiKey := req.Key
		if apiKey == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "No API key provided"})
			return nil
		}

		log.Printf("[Messages-Models] 请求模型列表: channel=%s, key=%s", channelName, utils.MaskAPIKey(apiKey))

		// 火山套餐渠道的数据面 /models 不反映套餐清单（/api/coding 返回全量目录、
		// /api/plan 无此接口），改走管控面套餐模型接口。
		if volcengineModels, handled, err := autopilot.FetchVolcenginePlanModelsForChannel(httpbridge.Request(c).Context(), cfgManager, channel, baseURL, apiKey); handled {
			if err != nil {
				if common.RespondNetworkEgressError(c, err) {
					return nil
				}
				log.Printf("[Messages-Models] 火山套餐模型查询失败: channel=%s, key=%s, error=%v", channelName, utils.MaskAPIKey(apiKey), err)
				httpbridge.JSON(c, http.StatusBadGateway, fiber.Map{"error": err.Error(), "statusCode": 502})
				return nil
			}
			data := make([]fiber.Map, 0, len(volcengineModels))
			for _, model := range volcengineModels {
				data = append(data, fiber.Map{"id": model})
			}
			httpbridge.JSON(c, http.StatusOK, fiber.Map{"data": data, "statusCode": http.StatusOK})
			return nil
		}

		// 5. 使用候选 URL 列表发起请求（messages 渠道自动尝试兼容路径）
		candidateURLs := buildClaudeCompatibleModelsURLs(baseURL)
		var copilotRuntimeToken string
		if serviceType == "copilot" {
			var copilotBaseURL string
			copilotRuntimeToken, copilotBaseURL, err = copilot.ResolveTokenWithProxy(httpclient.WithOutboundContext(httpbridge.Request(c).Context(), &config.UpstreamConfig{OutboundNetwork: outboundNetwork, ProxyURL: proxyURL, ProxyPreferDirect: proxyPreferDirect, InsecureSkipVerify: insecureSkipVerify}), apiKey, proxyURL)
			if err != nil {
				if common.RespondNetworkEgressError(c, err) {
					return nil
				}
				httpbridge.JSON(c, http.StatusBadGateway, fiber.Map{"error": fmt.Sprintf("Failed to exchange Copilot token: %v", err)})
				return nil
			}
			targetBaseURL := strings.TrimSuffix(strings.TrimSuffix(baseURL, "#"), "/")
			if copilotBaseURL != "" {
				targetBaseURL = strings.TrimRight(copilotBaseURL, "/")
			}
			candidateURLs = []string{targetBaseURL + "/models"}
		}

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

		var lastStatusCode int
		var lastBody []byte
		// Anthropic 风格端点（claude 系）或已学习客户端伪装标记的上游：
		// 请求带 Claude Code 探针头，避免被上游客户端指纹校验拒绝。
		useProbeHeaders := serviceType != "copilot" &&
			(strings.EqualFold(strings.TrimSpace(serviceType), "claude") || learnedClientFingerprint)
		for _, candidateURL := range candidateURLs {
			httpReq, err := http.NewRequestWithContext(httpbridge.Request(c).Context(), "GET", candidateURL, nil)
			if err != nil {
				if common.RespondNetworkEgressError(c, err) {
					return nil
				}
				log.Printf("[Messages-Models] 创建请求失败: channel=%s, url=%s, error=%v", channelName, candidateURL, err)
				continue
			}
			if serviceType == "copilot" {
				copilot.ApplyRuntimeHeaders(httpReq.Header, copilotRuntimeToken)
			} else {
				utils.SetAuthenticationHeaderWithOverride(httpReq.Header, apiKey, authHeader)
				if useProbeHeaders {
					utils.ApplyClaudeCodeProbeHeaders(httpReq.Header, "")
				}
			}
			httpReq.Header.Set("Content-Type", "application/json")
			if serviceType == "copilot" {
				utils.ApplyCustomHeadersProtected(httpReq.Header, req.CustomHeaders, utils.CopilotProtectedHeaders)
			} else {
				utils.ApplyCustomHeaders(httpReq.Header, req.CustomHeaders)
			}

			resp, err := client.Do(httpReq)
			if err != nil {
				if common.RespondNetworkEgressError(c, err) {
					return nil
				}
				log.Printf("[Messages-Models] 请求失败: channel=%s, key=%s, url=%s, error=%v",
					channelName, utils.MaskAPIKey(apiKey), candidateURL, err)
				continue
			}

			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				if common.RespondNetworkEgressError(c, err) {
					return nil
				}
				log.Printf("[Messages-Models] 读取响应失败: channel=%s, error=%v", channelName, err)
				continue
			}

			lastStatusCode = resp.StatusCode
			lastBody = body

			if resp.StatusCode == http.StatusOK {
				log.Printf("[Messages-Models] 上游响应: channel=%s, key=%s, status=%d, url=%s",
					channelName, utils.MaskAPIKey(apiKey), resp.StatusCode, candidateURL)
				httpbridge.
					// 透传上游响应体时注入真实状态码，供前端展示（不再硬编码 200）。
					Data(c, resp.StatusCode, "application/json", common.InjectUpstreamStatusCode(body, resp.StatusCode))
				return nil
			}

			// 401/403 认证失败不继续尝试其他候选 URL
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				log.Printf("[Messages-Models] 上游认证失败: channel=%s, key=%s, status=%d, url=%s",
					channelName, utils.MaskAPIKey(apiKey), resp.StatusCode, candidateURL)
				break
			}

			log.Printf("[Messages-Models] 上游返回非 200: channel=%s, key=%s, status=%d, url=%s",
				channelName, utils.MaskAPIKey(apiKey), resp.StatusCode, candidateURL)
		}

		// 所有候选 URL 均失败，返回最后一次上游响应
		if lastStatusCode == http.StatusUnauthorized {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{
				"error":      "上游 API Key 无效",
				"statusCode": 401,
				"details":    string(lastBody),
			})
			return nil
		}
		if lastBody != nil {
			httpbridge.Data(c, lastStatusCode, "application/json", lastBody)
			return nil
		}
		httpbridge.JSON(c, http.StatusBadGateway, fiber.Map{"error": "Failed to fetch models from all candidate URLs"})
		return nil
	}
}
