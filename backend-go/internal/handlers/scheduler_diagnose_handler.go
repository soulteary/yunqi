package handlers

import (
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/autopilot"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
)

type schedulerDiagnoseRequest struct {
	UserID             string                               `json:"userId"`
	Model              string                               `json:"model"`
	RoutePrefix        string                               `json:"routePrefix"`
	ChannelName        string                               `json:"channelName"`
	FailedChannels     []int                                `json:"failedChannels"`
	HasImageContent    bool                                 `json:"hasImageContent"`
	AgentRole          string                               `json:"agentRole"`
	ContextRequirement *schedulerDiagnoseContextRequirement `json:"contextRequirement"`
}

type schedulerDiagnoseContextRequirement struct {
	InputTokens                int  `json:"inputTokens"`
	OutputTokens               int  `json:"outputTokens"`
	RequiredTokens             int  `json:"requiredTokens"`
	MinimumContextWindowTokens int  `json:"minimumContextWindowTokens"`
	ExplicitOutputMax          bool `json:"explicitOutputMax"`
	SkipWindowValidation       bool `json:"skipWindowValidation"`
}

// DiagnoseSchedulerSelection 返回一次不发送上游请求的渠道选择诊断。
func DiagnoseSchedulerSelection(sch *scheduler.ChannelScheduler, kind scheduler.ChannelKind) fiber.Handler {
	return func(c fiber.Ctx) error {
		if sch == nil {
			httpbridge.JSON(c, http.StatusServiceUnavailable, fiber.Map{"error": "scheduler unavailable"})
			return nil
		}

		var req schedulerDiagnoseRequest
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "invalid request body"})
			return nil
		}

		failedChannels := make(map[int]bool, len(req.FailedChannels))
		for _, index := range req.FailedChannels {
			if index >= 0 {
				failedChannels[index] = true
			}
		}

		model := strings.TrimSpace(req.Model)
		agentRole := strings.TrimSpace(req.AgentRole)
		contextRequirement := diagnoseContextRequirement(req.ContextRequirement)
		selectionContext := autopilot.ContextWithRequestProfile(httpbridge.Request(c).Context(), buildSchedulerDiagnoseRequestProfile(kind, model, agentRole, req.HasImageContent, contextRequirement))

		result, err := sch.SelectChannelWithOptions(selectionContext, scheduler.SelectionOptions{
			UserID:             strings.TrimSpace(req.UserID),
			FailedChannels:     failedChannels,
			Kind:               kind,
			Model:              model,
			RoutePrefix:        strings.TrimSpace(req.RoutePrefix),
			ChannelName:        strings.TrimSpace(req.ChannelName),
			ContextRequirement: contextRequirement,
			HasImageContent:    req.HasImageContent,
			AgentRole:          agentRole,
			DryRun:             true,
		})
		if err != nil {
			resp := fiber.Map{
				"ok":    false,
				"kind":  kind,
				"error": err.Error(),
			}
			if trace, ok := scheduler.SelectionTraceFromError(err); ok {
				resp["summary"] = scheduler.FormatSelectionTraceSummary(trace, 8)
				resp["trace"] = trace
			}
			httpbridge.JSON(c, http.StatusOK, resp)
			return nil
		}

		trace := result.Trace
		httpbridge.JSON(c, http.StatusOK, fiber.Map{
			"ok":      true,
			"kind":    kind,
			"reason":  result.Reason,
			"summary": scheduler.FormatSelectionTraceSummary(trace, 8),
			"trace":   trace,
			"selected": fiber.Map{
				"channelIndex": result.ChannelIndex,
				"channelName":  selectedChannelName(result),
				"serviceType":  selectedServiceType(result),
			},
		})
		return nil
	}
}

func buildSchedulerDiagnoseRequestProfile(
	kind scheduler.ChannelKind,
	model string,
	agentRole string,
	hasImage bool,
	requirement *scheduler.ContextRequirement,
) autopilot.RequestProfile {
	inputTokens := 0
	if requirement != nil {
		inputTokens = requirement.InputTokens
	}
	return autopilot.BuildRequestProfile(autopilot.RequestProfileFeatures{
		Model:         model,
		ChannelKind:   string(kind),
		Operation:     "completion",
		AgentRole:     agentRole,
		HasImage:      hasImage,
		EstTokens:     inputTokens,
		ContextNeed:   inputTokens,
		VisionNeed:    hasImage,
		ImageGenNeed:  kind == scheduler.ChannelKindImages,
		EmbeddingNeed: kind == scheduler.ChannelKindVectors,
	})
}

func diagnoseContextRequirement(req *schedulerDiagnoseContextRequirement) *scheduler.ContextRequirement {
	if req == nil {
		return nil
	}
	return &scheduler.ContextRequirement{
		InputTokens:                req.InputTokens,
		OutputTokens:               req.OutputTokens,
		RequiredTokens:             req.RequiredTokens,
		MinimumContextWindowTokens: req.MinimumContextWindowTokens,
		ExplicitOutputMax:          req.ExplicitOutputMax,
		SkipWindowValidation:       req.SkipWindowValidation,
	}
}

func selectedChannelName(result *scheduler.SelectionResult) string {
	if result == nil || result.Upstream == nil {
		return ""
	}
	return result.Upstream.Name
}

func selectedServiceType(result *scheduler.SelectionResult) string {
	if result == nil || result.Upstream == nil {
		return ""
	}
	return result.Upstream.ServiceType
}
