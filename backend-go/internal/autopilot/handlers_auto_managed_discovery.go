package autopilot

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/utils"
)

func providerRouteForChannel(tmpl *config.ProviderTemplate, kind, serviceType string) (config.ProviderRoute, bool) {
	for _, route := range tmpl.AutoAddRoutes() {
		if route.ChannelKind == kind && route.ServiceType == serviceType {
			return route, true
		}
	}
	return config.ProviderRoute{}, false
}

func accountRouteSuffix(kind string) string {
	switch kind {
	case "messages":
		return "-claude"
	case "chat":
		return "-chat"
	case "responses":
		return "-codex"
	case "gemini":
		return "-gemini"
	default:
		return "-" + kind
	}
}

// validChannelKinds 合法的渠道类型集合。
var validChannelKinds = map[string]bool{
	"messages":  true,
	"chat":      true,
	"responses": true,
	"gemini":    true,
	"images":    true,
	"vectors":   true,
}

// extractKindFromPath 从请求路径中提取 kind。
// 路径格式：/api/{kind}/channels/...
func extractKindFromPath(c fiber.Ctx) string {
	path := httpbridge.Request(c).URL.Path
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// 路径格式：api/{kind}/channels/...
	// parts[0]="api", parts[1]=kind
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

func channelNameExists(channels []config.UpstreamConfig, name string) bool {
	for _, ch := range channels {
		if ch.Name == name {
			return true
		}
	}
	return false
}

type existingAutoAddChannel struct {
	Kind     string
	Index    int
	Upstream config.UpstreamConfig
}

var autoAddChannelKinds = []string{"messages", "chat", "responses", "gemini", "images", "vectors"}

func equivalentAutoAddURLIdentities(rawURL string) map[string]struct{} {
	identities := utils.BaseURLSiteIdentities(rawURL)
	result := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		result[identity] = struct{}{}
	}
	return result
}

func findExistingAutoAddChannels(cfg config.Config, baseURLs []string) []existingAutoAddChannel {
	inputIdentities := make(map[string]struct{})
	for _, baseURL := range baseURLs {
		for identity := range equivalentAutoAddURLIdentities(baseURL) {
			inputIdentities[identity] = struct{}{}
		}
	}
	if len(inputIdentities) == 0 {
		return nil
	}

	var matches []existingAutoAddChannel
	for _, kind := range autoAddChannelKinds {
		for index, upstream := range getChannelSlice(cfg, kind) {
			matched := false
			for _, baseURL := range upstream.GetAllBaseURLs() {
				for identity := range equivalentAutoAddURLIdentities(baseURL) {
					if _, exists := inputIdentities[identity]; exists {
						matches = append(matches, existingAutoAddChannel{Kind: kind, Index: index, Upstream: upstream})
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
		}
	}
	return matches
}

func findChannelIndexByUID(channels []config.UpstreamConfig, channelUID string) int {
	for i, ch := range channels {
		if ch.ChannelUID == channelUID {
			return i
		}
	}
	return -1
}

func triggerDiscoveryForChannel(deps *AutoManagedDeps, kind string, channelUID string) bool {
	if deps == nil || deps.Runner == nil || deps.CfgManager == nil || channelUID == "" {
		return false
	}
	cfg := deps.CfgManager.GetConfig()
	channels := getChannelSlice(cfg, kind)
	index := findChannelIndexByUID(channels, channelUID)
	if index < 0 || index >= len(channels) {
		return false
	}
	ch := channels[index]
	return deps.Runner.TriggerDiscovery(channelUID, &ch, deps.CfgManager)
}

// handleAutoDiscover POST /{kind}/channels/:id/auto-discover
// 重新触发发现。
func handleAutoDiscover(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		kind := extractKindFromPath(c)
		if !validChannelKinds[kind] {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": fmt.Sprintf("不支持的渠道类型: %s", kind)})
			return nil
		}

		cfg := deps.CfgManager.GetConfig()
		channels := getChannelSlice(cfg, kind)
		id, found := findChannelIndex(channels, httpbridge.Param(c, "id"))
		if !found {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "渠道不存在"})
			return nil
		}

		channel := channels[id]
		if deps.Runner == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "发现服务未就绪"})
			return nil
		}

		started, err := deps.Runner.TriggerDiscoveryWithStatus(channel.ChannelUID, &channel, deps.CfgManager)
		if err != nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": fmt.Sprintf("发现任务持久化失败: %v", err)})
			return nil
		}
		if !started {
			httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": "发现任务已在运行中"})
			return nil
		}

		log.Printf("[AutoManaged-Discover] 重新触发发现: kind=%s id=%d uid=%s", kind, id, channel.ChannelUID)

		// 「重新发现」的语义是刷新整个逻辑渠道：对同一逻辑渠道下的兄弟协议上游
		// 幂等联动触发（started=false 表示已在运行，不算错误），否则用户点一次按钮
		// 只有本条协议上游被刷新，chat/responses 等兄弟上游的清单会永久滞留。
		triggered := []fiber.Map{{"kind": kind, "channelUid": channel.ChannelUID}}
		currentCfg := deps.CfgManager.GetConfig()
		for _, siblingUID := range deps.CfgManager.LogicalSiblingChannelUIDs(channel.ChannelUID) {
			if siblingUID == channel.ChannelUID {
				continue
			}
			siblingIndex, siblingKind := findChannelIndexAndKind(currentCfg, siblingUID)
			siblingChannels := getChannelSlice(currentCfg, siblingKind)
			if siblingIndex < 0 || siblingIndex >= len(siblingChannels) {
				continue
			}
			sibling := siblingChannels[siblingIndex]
			if _, err := deps.Runner.TriggerDiscoveryWithStatus(siblingUID, &sibling, deps.CfgManager); err != nil {
				// 兄弟触发失败只记日志，不影响主渠道响应。
				log.Printf("[AutoManaged-Discover] 兄弟渠道 %s 触发发现失败: %v", siblingUID, err)
				continue
			}
			triggered = append(triggered, fiber.Map{"kind": siblingKind, "channelUid": siblingUID})
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"channelUid":       channel.ChannelUID,
			"discoveryStarted": true,
			"triggered":        triggered,
		})
		return nil
	}
}

// handleAutoStatus GET /{kind}/channels/:id/auto-status
// 返回自动托管状态和发现结果。
func handleAutoStatus(deps *AutoManagedDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		kind := extractKindFromPath(c)
		if !validChannelKinds[kind] {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": fmt.Sprintf("不支持的渠道类型: %s", kind)})
			return nil
		}

		cfg := deps.CfgManager.GetConfig()
		channels := getChannelSlice(cfg, kind)
		id, found := findChannelIndex(channels, httpbridge.Param(c, "id"))
		if !found {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "渠道不存在"})
			return nil
		}

		channel := channels[id]
		resp := AutoStatusResponse{
			AutoManaged:   channel.AutoManaged,
			AutoManagedAt: channel.AutoManagedAt,
		}

		// 附加发现状态
		if deps.Runner != nil && channel.ChannelUID != "" {
			task := deps.Runner.GetTask(channel.ChannelUID)
			if task != nil {
				info := &DiscoveryStatusInfo{
					Status:     task.Status,
					StartedAt:  task.StartedAt,
					FinishedAt: task.FinishedAt,
					Error:      task.Error,
				}
				for _, ep := range task.Endpoints {
					info.Endpoints = append(info.Endpoints, EndpointDiscoveryInfo{
						KeyMask:                  ep.KeyMask,
						BaseURL:                  ep.BaseURL,
						ModelsCount:              ep.ModelsCount,
						ProtocolOk:               ep.ProtocolOk,
						ModelDiscoverySource:     ep.ModelDiscoverySource,
						ModelDiscoveryMessage:    ep.ModelDiscoveryMessage,
						ModelsDiscoveredAt:       ep.ModelsDiscoveredAt,
						ProtocolModels:           cloneProtocolModels(ep.ProtocolModels),
						ProtocolDiscoveredAt:     cloneTimeMap(ep.ProtocolDiscoveredAt),
						ProtocolDiscoverySource:  cloneStringMap(ep.ProtocolDiscoverySource),
						ProtocolDiscoveryMessage: cloneStringMap(ep.ProtocolDiscoveryMessage),
						ProtocolDiscoveryError:   cloneStringMap(ep.ProtocolDiscoveryError),
					})
				}
				resp.Discovery = info
			}
		}
		httpbridge.JSON(c, http.StatusOK, resp)
		return nil
	}
}

// kindToDefaultServiceType 根据渠道类型推导默认 serviceType。
func kindToDefaultServiceType(kind string) string {
	switch kind {
	case "messages":
		return "claude"
	case "gemini":
		return "gemini"
	case "chat", "images":
		return "openai"
	case "responses":
		return "responses"
	case "vectors":
		return "openai"
	default:
		return "openai"
	}
}

// getChannelSlice 根据 kind 从 Config 获取对应的渠道切片。
func getChannelSlice(cfg config.Config, kind string) []config.UpstreamConfig {
	switch kind {
	case "messages":
		return cfg.Upstream
	case "chat":
		return cfg.ChatUpstream
	case "responses":
		return cfg.ResponsesUpstream
	case "gemini":
		return cfg.GeminiUpstream
	case "images":
		return cfg.ImagesUpstream
	case "vectors":
		return cfg.VectorsUpstream
	default:
		return nil
	}
}

// findChannelIndex 按 channelUID 或整数索引在渠道列表中查找。
// `:id` 参数可以是 channelUID（ch_xxx）或整数下标，优先匹配 channelUID。
func findChannelIndex(channels []config.UpstreamConfig, idStr string) (int, bool) {
	// 先尝试按 channelUID 匹配
	for i, ch := range channels {
		if ch.ChannelUID == idStr {
			return i, true
		}
	}
	// 再尝试整数下标
	id := 0
	for _, ch := range idStr {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		id = id*10 + int(ch-'0')
	}
	if id >= 0 && id < len(channels) {
		return id, true
	}
	return 0, false
}
