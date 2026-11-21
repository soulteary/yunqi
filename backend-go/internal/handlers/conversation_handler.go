package handlers

import (
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/conversation"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
)

type ConversationHandlerDeps struct {
	Tracker          *conversation.ConversationTracker
	OverrideManager  *conversation.OverrideManager
	ChannelScheduler *scheduler.ChannelScheduler
	ConfigManager    *config.ConfigManager
}

func GetConversations(deps *ConversationHandlerDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		kindFilter := httpbridge.Query(c, "kind")

		conversations := deps.Tracker.GetActiveConversations(kindFilter)

		// 同步清理孤儿 override（会话已过期但 override 仍存在的情况）
		if kindFilter == "" {
			activeIDs := make(map[string]bool, len(conversations))
			for _, conv := range conversations {
				activeIDs[conv.ID] = true
			}
			deps.OverrideManager.PurgeOrphans(activeIDs)
		}

		overrides := deps.OverrideManager.GetAllOverrides()

		overridesResponse := make(map[string]interface{})
		for id, override := range overrides {
			overridesResponse[id] = fiber.Map{
				"sequence":         override.Sequence,
				"hasMainSequence":  override.HasMainSequence,
				"subagentSequence": override.SubagentSequence,
				"setAt":            override.SetAt,
				"expiresAt":        override.ExpiresAt,
				"isPerpetual":      override.IsPerpetual,
			}
		}

		channelsByKind := fiber.Map{}
		for _, kind := range []scheduler.ChannelKind{
			scheduler.ChannelKindMessages,
			scheduler.ChannelKindChat,
			scheduler.ChannelKindImages,
			scheduler.ChannelKindResponses,
			scheduler.ChannelKindGemini,
		} {
			channelsByKind[string(kind)] = deps.ChannelScheduler.GetConversationChannelsByKind(kind)
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"conversations":  conversations,
			"total":          len(conversations),
			"overrides":      overridesResponse,
			"channelsByKind": channelsByKind,
		})
		return nil
	}
}

type SetOverrideRequest struct {
	Sequence              []conversation.ChannelEntry `json:"sequence"`
	SubagentSequence      []conversation.ChannelEntry `json:"subagentSequence,omitempty"`      // subagent 专用序列（可选）
	ClearSubagentSequence bool                        `json:"clearSubagentSequence,omitempty"` // 仅清除 subagent 专用序列
	Duration              *int                        `json:"duration,omitempty"`              // 秒；nil=系统默认；-1=永不恢复
}

func SetConversationOverride(deps *ConversationHandlerDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		convID := httpbridge.Param(c, "id")
		if convID == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "conversation id is required"})
			return nil
		}

		var req SetOverrideRequest
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "invalid request: " + err.Error()})
			return nil
		}

		conv, ok := deps.Tracker.GetConversation(convID)
		if !ok {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "conversation not found"})
			return nil
		}

		// 解析 duration：nil=系统默认，-1=永不恢复，>0=自定义秒数
		var overrideDuration time.Duration
		if req.Duration != nil {
			if *req.Duration == -1 {
				overrideDuration = -1
			} else if *req.Duration > 0 {
				overrideDuration = time.Duration(*req.Duration) * time.Second
			}
		}

		if len(req.Sequence) == 0 && len(req.SubagentSequence) == 0 && !req.ClearSubagentSequence {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "sequence or subagentSequence is required"})
			return nil
		}

		if len(req.Sequence) > 0 {
			err := deps.OverrideManager.SetOverride(convID, conv.Kind, conv.RawUserID, req.Sequence, overrideDuration)
			if err != nil {
				httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": err.Error()})
				return nil
			}
		}

		if len(req.SubagentSequence) > 0 {
			if saErr := deps.OverrideManager.SetSubagentOverride(convID, conv.Kind, conv.RawUserID, req.SubagentSequence, req.Sequence, overrideDuration); saErr != nil {
				httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": saErr.Error()})
				return nil
			}
		}

		if req.ClearSubagentSequence {
			deps.OverrideManager.ClearSubagentOverride(convID)
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"message":               "override set successfully",
			"conversationId":        convID,
			"sequence":              req.Sequence,
			"subagentSequence":      req.SubagentSequence,
			"clearSubagentSequence": req.ClearSubagentSequence,
		})
		return nil
	}
}

func RemoveConversationOverride(deps *ConversationHandlerDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		convID := httpbridge.Param(c, "id")
		if convID == "" {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "conversation id is required"})
			return nil
		}

		removed := deps.OverrideManager.RemoveOverride(convID)
		if !removed {
			httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "no override found for this conversation"})
			return nil
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"message":        "override removed",
			"conversationId": convID,
		})
		return nil
	}
}

// GetConversationSettings 获取驾驶舱设置
func GetConversationSettings(deps *ConversationHandlerDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		cfg := deps.ConfigManager.GetConfig()

		// 获取有效的 override TTL（优先使用配置文件中的值，否则返回 0 表示使用环境变量默认值）
		overrideTTLMinutes := cfg.OverrideTTLMinutes
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"overrideTtlMinutes": overrideTTLMinutes,
		})
		return nil
	}
}

type UpdateConversationSettingsRequest struct {
	OverrideTTLMinutes *int `json:"overrideTtlMinutes"` // 1-1440；nil 表示不修改
}

// UpdateConversationSettings 更新驾驶舱设置
func UpdateConversationSettings(deps *ConversationHandlerDeps) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req UpdateConversationSettingsRequest
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "invalid request: " + err.Error()})
			return nil
		}

		if req.OverrideTTLMinutes != nil {
			ttl := *req.OverrideTTLMinutes

			// 更新配置文件（内部会标准化为有效选项，不合适的值使用默认 30 分钟）
			if err := deps.ConfigManager.SetOverrideTTLMinutes(ttl); err != nil {
				httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": "failed to update config: " + err.Error()})
				return nil
			}

			// 获取标准化后的值
			cfg := deps.ConfigManager.GetConfig()
			normalizedTTL := cfg.OverrideTTLMinutes

			// 动态更新 OverrideManager 的默认 TTL
			if normalizedTTL == -1 {
				// -1 表示永不过期
				deps.OverrideManager.SetDefaultTTL(-1)
			} else {
				deps.OverrideManager.SetDefaultTTL(time.Duration(normalizedTTL) * time.Minute)
			}
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"message": "settings updated successfully",
		})
		return nil
	}
}
