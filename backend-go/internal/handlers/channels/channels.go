// Package channels 提供 Channel Data Model v2 的 REST API（只读 + 统一写端点）。
//
// 写端点内部按 kind 路由到现有的 ConfigManager 方法，在 ChannelUID 层面统一寻址，
// 避免前端直接调用 /api/{messages,chat,...}/channels/* 六套协议路由。
// 详见 docs/specs/channel-data-model-v2.md。
package channels

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
)

// Handler 渠道 v2 处理器。
type Handler struct {
	cm        *config.ConfigManager
	scheduler *scheduler.ChannelScheduler
}

// RegisterRoutes 注册渠道 v2 路由（读 + 写）。
func RegisterRoutes(apiGroup fiber.Router, cfgManager *config.ConfigManager, channelScheduler *scheduler.ChannelScheduler) {
	h := &Handler{cm: cfgManager, scheduler: channelScheduler}
	apiGroup.Get("/channels", h.List)
	apiGroup.Get("/channels/:uid", h.Get)
	apiGroup.Post("/channels/:uid/request-preview", h.RequestPreview)
	apiGroup.Get("/channels/:uid/model-import", h.GetModelImport)
	apiGroup.Put("/channels/:uid/model-import/policy", h.SetModelImportPolicy)
	apiGroup.Post("/channels/:uid/model-import/preview", h.PreviewModelImport)
	apiGroup.Post("/channels/:uid/model-import/approve", h.ApproveModelImport)
	apiGroup.Post("/channels", h.Create)
	apiGroup.Put("/channels/:uid", h.Update)
	apiGroup.Delete("/channels/:uid", h.Delete)
	apiGroup.Post("/channels/:uid/keys", h.AddKey)
	apiGroup.Delete("/channels/:uid/keys/:keyHash", h.RemoveKey)
}

// ListResponse 列表响应。
type ListResponse struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Channels      []config.ChannelView        `json:"channels"`
	Capabilities  []config.EndpointCapability `json:"capabilities"`
}

// List 返回全部渠道视图与共享能力。
func (h *Handler) List(c fiber.Ctx) error {
	views, caps := h.cm.GetChannelViews()
	httpbridge.JSON(c, http.StatusOK, ListResponse{
		SchemaVersion: config.ChannelSchemaVersion,
		Channels:      views,
		Capabilities:  caps,
	})
	return nil
}

// GetResponse 单渠道响应，仅附带该渠道 key 引用到的能力子集。
type GetResponse struct {
	Channel      config.ChannelView          `json:"channel"`
	Revision     string                      `json:"revision,omitempty"`
	Capabilities []config.EndpointCapability `json:"capabilities"`
}

// Get 按聚合 ChannelUID 返回单个渠道视图及其引用的能力子集。
func (h *Handler) Get(c fiber.Ctx) error {
	uid := httpbridge.Param(c, "uid")
	views, caps, revisions := h.cm.ChannelSaveViews()
	capByUID := make(map[string]config.EndpointCapability, len(caps))
	for _, cap := range caps {
		capByUID[cap.CapabilityUID] = cap
	}
	for _, v := range views {
		if v.ChannelUID != uid {
			continue
		}
		httpbridge.JSON(c, http.StatusOK, GetResponse{Channel: v, Revision: revisions[uid], Capabilities: referencedCapabilities(v, capByUID)})
		return nil
	}
	httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "channel not found", "uid": uid})
	return nil
}

// referencedCapabilities 收集某渠道所有 key endpoint 引用到的能力（保序去重）。
func referencedCapabilities(v config.ChannelView, byUID map[string]config.EndpointCapability) []config.EndpointCapability {
	seen := make(map[string]struct{})
	out := make([]config.EndpointCapability, 0)
	for _, k := range v.Keys {
		for _, e := range k.Endpoints {
			if _, ok := seen[e.CapabilityUID]; ok {
				continue
			}
			if cap, ok := byUID[e.CapabilityUID]; ok {
				seen[e.CapabilityUID] = struct{}{}
				out = append(out, cap)
			}
		}
	}
	return out
}

// CreateRequest 统一渠道创建请求体。
type CreateRequest struct {
	Kind                    string                        `json:"kind"` // messages|chat|responses|gemini|images|vectors
	Name                    string                        `json:"name"`
	CopilotProtocol         string                        `json:"copilotProtocol,omitempty"`
	ServiceType             string                        `json:"serviceType"`
	BaseURL                 string                        `json:"baseUrl"`
	BaseURLs                []string                      `json:"baseUrls"`
	APIKeys                 []string                      `json:"apiKeys"`
	APIKeyConfigs           []config.APIKeyConfig         `json:"apiKeyConfigs"`
	SupportedModels         []string                      `json:"supportedModels"`
	CustomHeaders           map[string]string             `json:"customHeaders"`
	ProxyURL                string                        `json:"proxyUrl"`
	ProxyPreferDirect       bool                          `json:"proxyPreferDirect"`
	OutboundNetwork         *config.OutboundNetworkConfig `json:"outboundNetwork,omitempty"`
	RoutePrefix             string                        `json:"routePrefix"`
	Status                  string                        `json:"status"`
	Placement               string                        `json:"placement"`
	AuthHeader              string                        `json:"authHeader"`
	InsecureSkipVerify      bool                          `json:"insecureSkipVerify"`
	RequestTimeoutMs        int                           `json:"requestTimeoutMs"`
	ResponseHeaderTimeoutMs int                           `json:"responseHeaderTimeoutMs"`
	NoVision                bool                          `json:"noVision"`
	FastMode                bool                          `json:"fastMode"`
	ResponsesReasoningFold  bool                          `json:"responsesReasoningFold"`
}

// Create 创建渠道（统一入口，按 kind 路由到对应数组）。
func (h *Handler) Create(c fiber.Ctx) error {
	var req CreateRequest
	if err := httpbridge.BindJSON(c, &req); err != nil {
		httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid request body: " + err.Error()})
		return nil
	}

	k, err := config.ChannelKindByName(req.Kind)
	if err != nil {
		httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
		return nil
	}

	up := config.UpstreamConfig{
		Name:                    req.Name,
		ServiceType:             req.ServiceType,
		CopilotProtocol:         req.CopilotProtocol,
		BaseURL:                 req.BaseURL,
		BaseURLs:                req.BaseURLs,
		APIKeys:                 req.APIKeys,
		APIKeyConfigs:           req.APIKeyConfigs,
		SupportedModels:         req.SupportedModels,
		CustomHeaders:           req.CustomHeaders,
		ProxyURL:                req.ProxyURL,
		ProxyPreferDirect:       req.ProxyPreferDirect,
		OutboundNetwork:         req.OutboundNetwork,
		RoutePrefix:             req.RoutePrefix,
		Status:                  req.Status,
		AuthHeader:              req.AuthHeader,
		InsecureSkipVerify:      req.InsecureSkipVerify,
		RequestTimeoutMs:        req.RequestTimeoutMs,
		ResponseHeaderTimeoutMs: req.ResponseHeaderTimeoutMs,
		NoVision:                req.NoVision,
		FastMode:                req.FastMode,
		ResponsesReasoningFold:  req.ResponsesReasoningFold,
	}

	if err := config.AddUpstreamByKind(h.cm, k, up, req.Placement); err != nil {
		log.Printf("[Channels-Create] 创建渠道失败: %v", err)
		httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": "Failed to create channel: " + err.Error()})
		return nil
	}
	httpbridge.JSON(c, http.StatusOK, fiber.Map{"message": "渠道已创建"})
	return nil
}

// UpdateRequest 统一渠道更新请求体。
type UpdateRequest struct {
	NullFields                    []string                                  `json:"-"`
	Kind                          *string                                   `json:"kind,omitzero"`
	Name                          *string                                   `json:"name,omitzero"`
	CopilotProtocol               *string                                   `json:"copilotProtocol,omitzero"`
	ServiceType                   *string                                   `json:"serviceType,omitzero"`
	BaseURL                       *string                                   `json:"baseUrl,omitzero"`
	BaseURLs                      []string                                  `json:"baseUrls,omitzero"`
	APIKeys                       []string                                  `json:"apiKeys,omitzero"`
	APIKeyConfigs                 []config.APIKeyConfig                     `json:"apiKeyConfigs,omitzero"`
	ModelCapabilities             map[string]config.UpstreamModelCapability `json:"modelCapabilities,omitzero"`
	SupportedModels               []string                                  `json:"supportedModels,omitzero"`
	CustomHeaders                 map[string]string                         `json:"customHeaders,omitzero"`
	ProxyURL                      *string                                   `json:"proxyUrl,omitzero"`
	ProxyPreferDirect             *bool                                     `json:"proxyPreferDirect,omitzero"`
	OutboundNetwork               config.OutboundNetworkPatch               `json:"outboundNetwork,omitzero"`
	RoutePrefix                   *string                                   `json:"routePrefix,omitzero"`
	Status                        *string                                   `json:"status,omitzero"`
	Priority                      *int                                      `json:"priority,omitzero"`
	AuthHeader                    *string                                   `json:"authHeader,omitzero"`
	InsecureSkipVerify            *bool                                     `json:"insecureSkipVerify,omitzero"`
	RequestTimeoutMs              *int                                      `json:"requestTimeoutMs,omitzero"`
	ResponseHeaderTimeoutMs       *int                                      `json:"responseHeaderTimeoutMs,omitzero"`
	NoVision                      *bool                                     `json:"noVision,omitzero"`
	FastMode                      *bool                                     `json:"fastMode,omitzero"`
	Remark                        *string                                   `json:"remark,omitzero"`
	Description                   *string                                   `json:"description,omitzero"`
	Website                       *string                                   `json:"website,omitzero"`
	Tags                          []string                                  `json:"tags,omitzero"`
	RateLimitRPM                  *int                                      `json:"rateLimitRpm,omitzero"`
	RateLimitBurst                *int                                      `json:"rateLimitBurst,omitzero"`
	RateLimitMaxConcurrent        *int                                      `json:"rateLimitMaxConcurrent,omitzero"`
	ConvertImageURLToB64JSON      *bool                                     `json:"convertImageUrlToB64Json,omitzero"`
	StripCodexClientTools         *bool                                     `json:"stripCodexClientTools,omitzero"`
	CodexToolCompat               *bool                                     `json:"codexToolCompat,omitzero"`
	ResponsesReasoningFold        *bool                                     `json:"responsesReasoningFold,omitzero"`
	EmbeddingCapabilities         map[string]config.EmbeddingCapability     `json:"embeddingCapabilities,omitzero"`
	DefaultCapability             *config.UpstreamModelCapability           `json:"defaultCapability,omitzero"`
	AllowUnknownContext           *bool                                     `json:"allowUnknownContext,omitzero"`
	ReasoningParamStyle           *string                                   `json:"reasoningParamStyle,omitzero"`
	TextVerbosity                 *string                                   `json:"textVerbosity,omitzero"`
	Racing                        *config.ChannelRacingConfig               `json:"racing,omitzero"`
	LowQuality                    *bool                                     `json:"lowQuality,omitzero"`
	AutoBlacklistBalance          *bool                                     `json:"autoBlacklistBalance,omitzero"`
	NormalizeMetadataUserID       *bool                                     `json:"normalizeMetadataUserId,omitzero"`
	StripBillingHeader            *bool                                     `json:"stripBillingHeader,omitzero"`
	NormalizeSystemRoleToTopLevel *bool                                     `json:"normalizeSystemRoleToTopLevel,omitzero"`
	InjectDummyThoughtSignature   *bool                                     `json:"injectDummyThoughtSignature,omitzero"`
	StripThoughtSignature         *bool                                     `json:"stripThoughtSignature,omitzero"`
	StreamFirstContentTimeoutMs   *int                                      `json:"streamFirstContentTimeoutMs,omitzero"`
	StreamInactivityTimeoutMs     *int                                      `json:"streamInactivityTimeoutMs,omitzero"`
	StreamToolCallIdleTimeoutMs   *int                                      `json:"streamToolCallIdleTimeoutMs,omitzero"`
	RateLimitWindowMinutes        *int                                      `json:"rateLimitWindowMinutes,omitzero"`
	RateLimitAutoFromHeaders      *bool                                     `json:"rateLimitAutoFromHeaders,omitzero"`
	CostMultiplier                *float64                                  `json:"costMultiplier,omitzero"`
	ChannelPaymentCurrency        *string                                   `json:"channelPaymentCurrency,omitzero"`
	ChannelPaymentAmount          *float64                                  `json:"channelPaymentAmount,omitzero"`
	ChannelCreditCurrency         *string                                   `json:"channelCreditCurrency,omitzero"`
	ChannelCreditAmount           *float64                                  `json:"channelCreditAmount,omitzero"`
	MaxGroupMultiplier            *float64                                  `json:"maxGroupMultiplier,omitzero"`
	HistoricalImageTurnLimit      *int                                      `json:"historicalImageTurnLimit,omitzero"`
	BalanceCheck                  *config.ChannelBalanceCheckConfig         `json:"balanceCheck,omitzero"`
}

// Update 更新渠道（按 ChannelUID 寻址，内部按 kind 路由）。
func (h *Handler) Update(c fiber.Ctx) error {
	uid := httpbridge.Param(c, "uid")
	_, _, found := h.cm.FindUpstreamByUID(uid)
	if !found {
		httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "channel not found", "uid": uid})
		return nil
	}

	var req UpdateRequest
	if err := httpbridge.BindJSON(c, &req); err != nil {
		httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid request body: " + err.Error()})
		return nil
	}

	updates := config.UpstreamUpdate{
		NullFields:                    req.NullFields,
		Name:                          req.Name,
		ServiceType:                   req.ServiceType,
		CopilotProtocol:               req.CopilotProtocol,
		BaseURL:                       req.BaseURL,
		BaseURLs:                      req.BaseURLs,
		APIKeys:                       req.APIKeys,
		APIKeyConfigs:                 req.APIKeyConfigs,
		ModelCapabilities:             req.ModelCapabilities,
		SupportedModels:               req.SupportedModels,
		CustomHeaders:                 req.CustomHeaders,
		ProxyURL:                      req.ProxyURL,
		ProxyPreferDirect:             req.ProxyPreferDirect,
		OutboundNetwork:               req.OutboundNetwork,
		RoutePrefix:                   req.RoutePrefix,
		Status:                        req.Status,
		Priority:                      req.Priority,
		AuthHeader:                    req.AuthHeader,
		InsecureSkipVerify:            req.InsecureSkipVerify,
		RequestTimeoutMs:              req.RequestTimeoutMs,
		ResponseHeaderTimeoutMs:       req.ResponseHeaderTimeoutMs,
		NoVision:                      req.NoVision,
		FastMode:                      req.FastMode,
		Remark:                        req.Remark,
		Description:                   req.Description,
		Website:                       req.Website,
		Tags:                          req.Tags,
		RateLimitRPM:                  req.RateLimitRPM,
		RateLimitBurst:                req.RateLimitBurst,
		RateLimitMaxConcurrent:        req.RateLimitMaxConcurrent,
		ConvertImageURLToB64JSON:      req.ConvertImageURLToB64JSON,
		StripCodexClientTools:         req.StripCodexClientTools,
		CodexToolCompat:               req.CodexToolCompat,
		ResponsesReasoningFold:        req.ResponsesReasoningFold,
		EmbeddingCapabilities:         req.EmbeddingCapabilities,
		DefaultCapability:             req.DefaultCapability,
		AllowUnknownContext:           req.AllowUnknownContext,
		ReasoningParamStyle:           req.ReasoningParamStyle,
		TextVerbosity:                 req.TextVerbosity,
		Racing:                        req.Racing,
		LowQuality:                    req.LowQuality,
		AutoBlacklistBalance:          req.AutoBlacklistBalance,
		NormalizeMetadataUserID:       req.NormalizeMetadataUserID,
		StripBillingHeader:            req.StripBillingHeader,
		NormalizeSystemRoleToTopLevel: req.NormalizeSystemRoleToTopLevel,
		InjectDummyThoughtSignature:   req.InjectDummyThoughtSignature,
		StripThoughtSignature:         req.StripThoughtSignature,
		StreamFirstContentTimeoutMs:   req.StreamFirstContentTimeoutMs,
		StreamInactivityTimeoutMs:     req.StreamInactivityTimeoutMs,
		StreamToolCallIdleTimeoutMs:   req.StreamToolCallIdleTimeoutMs,
		RateLimitWindowMinutes:        req.RateLimitWindowMinutes,
		RateLimitAutoFromHeaders:      req.RateLimitAutoFromHeaders,
		CostMultiplier:                req.CostMultiplier,
		ChannelPaymentCurrency:        req.ChannelPaymentCurrency,
		ChannelPaymentAmount:          req.ChannelPaymentAmount,
		ChannelCreditCurrency:         req.ChannelCreditCurrency,
		ChannelCreditAmount:           req.ChannelCreditAmount,
		MaxGroupMultiplier:            req.MaxGroupMultiplier,
		HistoricalImageTurnLimit:      req.HistoricalImageTurnLimit,
		BalanceCheck:                  req.BalanceCheck,
	}

	receipt, loc, shouldResetMetrics, err := h.cm.UpdateChannelWithReceipt(uid, httpbridge.Request(c).Header.Get("If-Match"), updates)
	if err != nil {
		if errors.Is(err, config.ErrChannelRevisionConflict) {
			httpbridge.JSON(c, http.StatusConflict, fiber.Map{"error": err.Error(), "persisted": false})
			return nil
		}
		log.Printf("[Channels-Update] 更新渠道失败: %v", err)
		httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": "Failed to update channel: " + err.Error()})
		return nil
	}

	if shouldResetMetrics && h.scheduler != nil {
		kind := schedulerChannelKind(loc.Kind)
		h.scheduler.ResetChannelMetrics(loc.Index, kind)
	}
	httpbridge.JSON(c, http.StatusOK, fiber.Map{"message": "渠道已更新", "revision": receipt.Revision, "persisted": receipt.Persisted, "changed": receipt.Changed, "changedFields": receipt.ChangedFields})
	return nil
}

// Delete 删除渠道（按 ChannelUID 寻址）。
func (h *Handler) Delete(c fiber.Ctx) error {
	uid := httpbridge.Param(c, "uid")
	loc, _, found := h.cm.FindUpstreamByUID(uid)
	if !found {
		httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "channel not found", "uid": uid})
		return nil
	}

	removed, err := config.RemoveUpstreamByKind(h.cm, loc)
	if err != nil {
		log.Printf("[Channels-Delete] 删除渠道失败: %v", err)
		httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": "Failed to delete channel: " + err.Error()})
		return nil
	}

	if h.scheduler != nil {
		kind := schedulerChannelKind(loc.Kind)
		h.scheduler.DeleteChannelLogs(removed, kind)
		h.scheduler.DeleteChannelMetrics(removed, kind)
	}
	httpbridge.JSON(c, http.StatusOK, fiber.Map{"message": "渠道已删除", "removed": removed})
	return nil
}

// AddKeyRequest 添加 key 请求体。
type AddKeyRequest struct {
	APIKey string `json:"apiKey"`
}

// AddKey 添加 API Key（按 ChannelUID 寻址）。
func (h *Handler) AddKey(c fiber.Ctx) error {
	uid := httpbridge.Param(c, "uid")
	loc, _, found := h.cm.FindUpstreamByUID(uid)
	if !found {
		httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "channel not found", "uid": uid})
		return nil
	}

	var req AddKeyRequest
	if err := httpbridge.BindJSON(c, &req); err != nil {
		httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "Invalid request body: " + err.Error()})
		return nil
	}
	if strings.TrimSpace(req.APIKey) == "" {
		httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "apiKey is required"})
		return nil
	}

	if err := config.AddChannelKeyByKind(h.cm, loc, req.APIKey); err != nil {
		if strings.Contains(err.Error(), "API密钥已存在") {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "API密钥已存在"})
		} else {
			log.Printf("[Channels-Key] 添加 key 失败: %v", err)
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": "Failed to add key: " + err.Error()})
		}
		return nil
	}
	httpbridge.JSON(c, http.StatusOK, fiber.Map{"message": "API密钥已添加", "success": true})
	return nil
}

// RemoveKey 删除 API Key（按 ChannelUID + keyHash 寻址，keyHash 是 key 的前 16 位 hex）。
func (h *Handler) RemoveKey(c fiber.Ctx) error {
	uid := httpbridge.Param(c, "uid")
	keyHash := httpbridge.Param(c, "keyHash")
	loc, upstream, found := h.cm.FindUpstreamByUID(uid)
	if !found {
		httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "channel not found", "uid": uid})
		return nil
	}

	// keyHash 是 key 的 SHA256 前 16 位 hex，查找匹配的明文 key
	var matchedKey string
	for _, k := range upstream.APIKeys {
		if config.ChannelKeyHash(k) == keyHash {
			matchedKey = k
			break
		}
	}
	if matchedKey == "" {
		// 也检查历史 key
		for _, k := range upstream.HistoricalAPIKeys {
			if config.ChannelKeyHash(k) == keyHash {
				matchedKey = k
				break
			}
		}
	}
	if matchedKey == "" {
		httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "key not found", "keyHash": keyHash})
		return nil
	}

	if err := config.RemoveChannelKeyByKind(h.cm, loc, matchedKey); err != nil {
		if strings.Contains(err.Error(), "API密钥不存在") {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "API key not found"})
		} else {
			log.Printf("[Channels-Key] 删除 key 失败: %v", err)
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": "Failed to remove key: " + err.Error()})
		}
		return nil
	}
	httpbridge.JSON(c, http.StatusOK, fiber.Map{"message": "API密钥已删除"})
	return nil
}

// schedulerChannelKind 将 config.channelKind 转为 scheduler.ChannelKind。
func schedulerChannelKind(k config.ChannelKind) scheduler.ChannelKind {
	switch k {
	case config.ChannelKindMessages:
		return scheduler.ChannelKindMessages
	case config.ChannelKindChat:
		return scheduler.ChannelKindChat
	case config.ChannelKindResponses:
		return scheduler.ChannelKindResponses
	case config.ChannelKindGemini:
		return scheduler.ChannelKindGemini
	case config.ChannelKindImages:
		return scheduler.ChannelKindImages
	case config.ChannelKindVectors:
		return scheduler.ChannelKindVectors
	default:
		return scheduler.ChannelKindMessages
	}
}

func (r *UpdateRequest) UnmarshalJSON(data []byte) error {
	type wire UpdateRequest
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	fields, err := config.ParseChannelNullFields(data)
	if err != nil {
		return err
	}
	*r = UpdateRequest(decoded)
	r.NullFields = fields
	return nil
}
