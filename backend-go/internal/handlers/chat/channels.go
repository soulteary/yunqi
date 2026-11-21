// Package chat 提供 Chat Completions API 的渠道管理
package chat

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/autopilot"
	"github.com/soulteary/yunqi/internal/config"
	handlers "github.com/soulteary/yunqi/internal/handlers"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/httpclient"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/utils"
)

// GetUpstreams 获取 Chat 上游列表
func GetUpstreams(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		cfg := cfgManager.GetConfig()

		upstreams := make([]fiber.Map, len(cfg.ChatUpstream))
		for i, up := range cfg.ChatUpstream {
			upstreams[i] = common.BuildChannelView(up, i)
		}
		httpbridge.JSON(c, 200, fiber.Map{
			"channels": upstreams,
		})
		return nil
	}
}

// AddUpstream 添加 Chat 上游
func AddUpstream(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req struct {
			config.UpstreamConfig
			Placement string `json:"placement"` // 故障转移位置：front（首位）| back（末尾，默认）
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}

		if err := cfgManager.AddChatUpstream(req.UpstreamConfig, req.Placement); err != nil {
			httpbridge.JSON(c, 500, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"message": "Chat upstream added successfully"})
		return nil
	}
}

// UpdateUpstream 更新 Chat 上游
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
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}

		oldName := ""
		{
			cfg := cfgManager.GetConfig()
			if id >= 0 && id < len(cfg.ChatUpstream) {
				oldName = cfg.ChatUpstream[id].Name
			}
		}

		shouldResetMetrics, err := cfgManager.UpdateChatUpstream(id, updates)
		if err != nil {
			httpbridge.JSON(c, 500, fiber.Map{"error": err.Error()})
			return nil
		}

		// 名称由后端按首个 baseURL 派生：baseURL 顺序或首地址变化可能触发改名，需迁移日志归属
		newName := ""
		{
			cfg := cfgManager.GetConfig()
			if id >= 0 && id < len(cfg.ChatUpstream) {
				newName = cfg.ChatUpstream[id].Name
			}
		}
		if oldName != "" && newName != "" && oldName != newName {
			if logStore := sch.GetChannelLogStore(scheduler.ChannelKindChat); logStore != nil {
				logStore.RenameChannel(oldName, newName)
			}
		}

		// 单 key 更换时重置熔断状态
		if shouldResetMetrics {
			sch.ResetChannelMetrics(id, scheduler.ChannelKindChat)
		}
		httpbridge.JSON(c, 200, fiber.Map{"message": "Chat upstream updated successfully"})
		return nil
	}
}

// DeleteUpstream 删除 Chat 上游
func DeleteUpstream(cfgManager *config.ConfigManager, channelScheduler *scheduler.ChannelScheduler) fiber.Handler {
	return func(c fiber.Ctx) error {
		idStr := httpbridge.Param(c, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid upstream ID"})
			return nil
		}

		removed, err := cfgManager.RemoveChatUpstream(id)
		if err != nil {
			if strings.Contains(err.Error(), "无效的") {
				httpbridge.JSON(c, 404, fiber.Map{"error": "Upstream not found"})
			} else {
				httpbridge.JSON(c, 500, fiber.Map{"error": err.Error()})
			}
			return nil
		}

		channelScheduler.DeleteChannelLogs(removed, scheduler.ChannelKindChat)
		channelScheduler.DeleteChannelMetrics(removed, scheduler.ChannelKindChat)
		httpbridge.JSON(c, 200, fiber.Map{"message": "Chat upstream deleted successfully"})
		return nil
	}
}

// AddApiKey 添加 Chat 渠道 API 密钥
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

		if err := cfgManager.AddChatAPIKey(id, req.APIKey); err != nil {
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

// DeleteApiKey 删除 Chat 渠道 API 密钥
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

		if err := cfgManager.RemoveChatAPIKey(id, apiKey); err != nil {
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

// MoveApiKeyToTop 将 Chat 渠道 API 密钥移到最前面
func MoveApiKeyToTop(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, _ := strconv.Atoi(httpbridge.Param(c, "id"))
		apiKey := httpbridge.Param(c, "apiKey")

		if err := cfgManager.MoveChatAPIKeyToTop(id, apiKey); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"message": "API密钥已置顶"})
		return nil
	}
}

// MoveApiKeyToBottom 将 Chat 渠道 API 密钥移到最后面
func MoveApiKeyToBottom(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, _ := strconv.Atoi(httpbridge.Param(c, "id"))
		apiKey := httpbridge.Param(c, "apiKey")

		if err := cfgManager.MoveChatAPIKeyToBottom(id, apiKey); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"message": "API密钥已置底"})
		return nil
	}
}

// ReorderChannels 重新排序 Chat 渠道优先级
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

		if err := cfgManager.ReorderChatUpstreams(req.Order, req.Priorities); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": err.Error()})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{
			"success": true,
			"message": "Chat 渠道优先级已更新",
		})
		return nil
	}
}

// SetChannelStatus 设置 Chat 渠道状态
func SetChannelStatus(cfgManager *config.ConfigManager) fiber.Handler {
	adapter := handlers.ChannelStatusConfigManagerFunc(func(index int, status string) error {
		return cfgManager.SetChatChannelStatus(index, status)
	})
	return handlers.NamedChannelStatusHandler(adapter, "Chat 渠道状态已更新")
}

// SetChannelPromotion 设置 Chat 渠道促销期
func SetChannelPromotion(cfgManager *config.ConfigManager) fiber.Handler {
	adapter := handlers.PromotionConfigManagerFunc(func(index int, duration time.Duration) error {
		return cfgManager.SetChatChannelPromotion(index, duration)
	})
	return handlers.NamedChannelPromotionHandler(adapter, "Invalid channel ID", "Invalid request body", "Chat 渠道促销期已清除", "Chat 渠道促销期已设置")
}

// PingChannel 测试 Chat 渠道连通性
func PingChannel(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		idStr := httpbridge.Param(c, "id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "Invalid channel ID"})
			return nil
		}

		cfg := cfgManager.GetConfig()
		if id < 0 || id >= len(cfg.ChatUpstream) {
			httpbridge.JSON(c, 404, fiber.Map{"error": "Channel not found"})
			return nil
		}
		httpbridge.JSON(c, 200, common.PingSingleBaseURLUpstream(cfg.ChatUpstream[id], buildPingRequest))
		return nil
	}
}

// PingAllChannels 测试所有 Chat 渠道连通性
func PingAllChannels(cfgManager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		cfg := cfgManager.GetConfig()
		httpbridge.JSON(c, 200, fiber.Map{"channels": common.PingAllSingleBaseURLUpstreams(cfg.ChatUpstream, buildPingRequest, true)["channels"]})
		return nil
	}
}

func buildPingRequest(upstream config.UpstreamConfig, baseURL string) (*http.Request, error) {
	var req *http.Request
	switch upstream.ServiceType {
	case "claude":
		req, _ = http.NewRequest(http.MethodOptions, buildMessagesURL(baseURL), nil)
		if len(upstream.APIKeys) > 0 {
			utils.SetAuthenticationHeaderWithOverride(req.Header, upstream.APIKeys[0], upstream.AuthHeader)
			req.Header.Set("anthropic-version", "2023-06-01")
		}
	default:
		req, _ = http.NewRequest(http.MethodGet, buildModelsURL(baseURL), nil)
		if len(upstream.APIKeys) > 0 {
			utils.SetAuthenticationHeaderWithOverride(req.Header, upstream.APIKeys[0], upstream.AuthHeader)
		}
	}
	return req, nil
}

// buildEndpointURL 构建带版本前缀的端点 URL
func buildEndpointURL(baseURL, versionPrefix, endpoint string) string {
	skipVersionPrefix := strings.HasSuffix(baseURL, "#")
	if skipVersionPrefix {
		baseURL = strings.TrimSuffix(baseURL, "#")
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	versionPattern := regexp.MustCompile(`/v\d+[a-z]*$`)
	hasVersionSuffix := versionPattern.MatchString(baseURL)

	if !hasVersionSuffix && !skipVersionPrefix {
		baseURL += versionPrefix
	}

	return baseURL + endpoint
}

func buildMessagesURL(baseURL string) string {
	return buildEndpointURL(baseURL, "/v1", "/messages")
}

// buildModelsURL 构建 models 端点的 URL
func buildModelsURL(baseURL string) string {
	if modelsURL, ok := config.ResolveBuiltinModelsURL(baseURL, "openai"); ok {
		return modelsURL
	}
	return buildEndpointURL(baseURL, "/v1", "/models")
}

// GetModelsRequest 获取模型列表的请求体
type GetModelsRequest struct {
	Key      string   `json:"key"`
	BaseURL  string   `json:"baseUrl"`
	BaseURLs []string `json:"baseUrls"`
	ProxyURL string   `json:"proxyUrl"`
	// ProxyPreferDirect 直连优先（nil=未指定，回退渠道配置）；仅在配置了代理时有意义
	ProxyPreferDirect  *bool             `json:"proxyPreferDirect"`
	InsecureSkipVerify *bool             `json:"insecureSkipVerify"`
	CustomHeaders      map[string]string `json:"customHeaders"`
	AuthHeader         string            `json:"authHeader"`
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
		var insecureSkipVerify bool
		var proxyURL string
		var proxyPreferDirect bool
		var authHeader string
		var learnedClientFingerprint bool

		if req.BaseURL != "" {
			// 新增模式：使用临时 baseUrl
			// SSRF 防护：验证用户提供的 baseURL
			if err := utils.ValidateBaseURL(req.BaseURL); err != nil {
				log.Printf("[Chat-Models] SSRF 防护拦截: %v", err)
				httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": fmt.Sprintf("无效的 baseUrl: %v", err)})
				return nil
			}
			baseURL = req.BaseURL
			channelName = "临时渠道"
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
			log.Printf("[Chat-Models] 使用临时 baseUrl: %s", baseURL)
		} else {
			// 编辑模式：从配置中读取渠道信息
			cfg := cfgManager.GetConfig()
			if id < 0 || id >= len(cfg.ChatUpstream) {
				httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "Channel not found"})
				return nil
			}

			channel = &cfg.ChatUpstream[id]
			baseURL = channel.BaseURL
			channelName = channel.Name
			insecureSkipVerify = channel.InsecureSkipVerify
			proxyURL = channel.ProxyURL
			proxyPreferDirect = channel.ProxyPreferDirect
			authHeader = channel.AuthHeader
			learnedClientFingerprint = channel.LearnedClientFingerprint || req.LearnedClientFingerprint
			if req.BaseURL != "" {
				if err := utils.ValidateBaseURL(req.BaseURL); err != nil {
					log.Printf("[Chat-Models] SSRF 防护拦截: %v", err)
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
		}

		// 4. 验证 API Key
		apiKey := req.Key
		if apiKey == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "No API key provided"})
			return nil
		}

		log.Printf("[Chat-Models] 请求模型列表: channel=%s, key=%s", channelName, utils.MaskAPIKey(apiKey))

		// 火山套餐渠道的数据面 /models 不反映套餐清单（/api/coding 返回全量目录、
		// /api/plan 无此接口），改走管控面套餐模型接口。
		if volcengineModels, handled, err := autopilot.FetchVolcenginePlanModelsForChannel(httpbridge.Request(c).Context(), cfgManager, channel, baseURL, apiKey); handled {
			if err != nil {
				log.Printf("[Chat-Models] 火山套餐模型查询失败: channel=%s, key=%s, error=%v", channelName, utils.MaskAPIKey(apiKey), err)
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

		// 5. 发起请求
		url := buildModelsURL(baseURL)
		clientOpts := httpclient.ClientOptions{
			Timeout:           10 * time.Second,
			Insecure:          insecureSkipVerify,
			ProxyURL:          proxyURL,
			ProxyPreferDirect: proxyPreferDirect,
		}
		client := httpclient.GetManager().GetClient(clientOpts)
		if req.BaseURL != "" && req.ProxyURL != "" {
			// 新增模式下的临时代理参数高变，不进缓存
			client = httpclient.GetManager().NewClient(clientOpts)
		}

		httpReq, err := http.NewRequestWithContext(httpbridge.Request(c).Context(), "GET", url, nil)
		if err != nil {
			log.Printf("[Chat-Models] 创建请求失败: channel=%s, url=%s, error=%v", channelName, url, err)
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

		resp, err := client.Do(httpReq)
		if err != nil {
			log.Printf("[Chat-Models] 请求失败: channel=%s, key=%s, url=%s, error=%v",
				channelName, utils.MaskAPIKey(apiKey), url, err)
			httpbridge.JSON(c, http.StatusBadGateway, fiber.Map{"error": fmt.Sprintf("Failed to fetch models: %v", err)})
			return nil
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			log.Printf("[Chat-Models] 读取响应失败: channel=%s, error=%v", channelName, err)
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": fmt.Sprintf("Failed to read response: %v", err)})
			return nil
		}

		log.Printf("[Chat-Models] 上游响应: channel=%s, key=%s, status=%d, url=%s",
			channelName, utils.MaskAPIKey(apiKey), resp.StatusCode, url)
		// 包装上游 401 错误，避免前端误判为管理 API 认证失败
		if resp.StatusCode == 401 {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{
				"error":      "上游 API Key 无效",
				"statusCode": 401,
				"details":    string(body),
			})
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
