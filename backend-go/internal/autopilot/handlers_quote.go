package autopilot

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/tidwall/gjson"
)

// CallerQuoteHandler reuses route-preview's feature extraction and read-only
// scheduler path, while returning only caller-visible accounting information.
func CallerQuoteHandler(cm *config.ConfigManager, sch *scheduler.ChannelScheduler) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req struct {
			RoutePreviewRequest
			InputTokens  *int64 `json:"inputTokens"`
			OutputTokens *int64 `json:"outputTokens"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_quote_request"})
			return nil
		}
		kind := scheduler.ChannelKind(strings.TrimSpace(req.ChannelKind))
		body := bytes.TrimSpace(req.Body)
		if !isValidRoutePreviewKind(kind) || (len(body) > 0 && (body[0] != '{' || !json.Valid(body))) {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_quote_request"})
			return nil
		}
		if cm == nil || sch == nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "quote_unavailable"})
			return nil
		}
		model := strings.TrimSpace(req.Model)
		if model == "" {
			model = extractModelFromBody(body)
		}
		if model == "" {
			httpbridge.JSON(c, 400, fiber.Map{"error": "model_required"})
			return nil
		}
		cfg := cm.GetConfig()
		profile := buildRoutePreviewProfile(c, kind, model, normalizeRoutePreviewOperation(kind, strings.TrimSpace(req.Operation), c), body, cfg.AutopilotRouting.Scenario)
		input := int64(profile.ContextNeed)
		if req.InputTokens != nil {
			input = *req.InputTokens
		}
		output := int64(0)
		outputKnown := false
		if req.OutputTokens != nil {
			output = *req.OutputTokens
			outputKnown = true
		} else {
			for _, key := range []string{"max_output_tokens", "max_completion_tokens", "max_tokens", "generationConfig.maxOutputTokens"} {
				if value := gjson.GetBytes(body, key); value.Exists() {
					output = value.Int()
					outputKnown = true
					break
				}
			}
		}
		if input < 0 || output < 0 || input > 1_000_000_000 || output > 1_000_000_000 {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_token_count"})
			return nil
		}
		profile.ContextNeed = int(input)
		result, err := sch.SelectChannelWithOptions(ContextWithRequestProfile(httpbridge.Request(c).Context(), profile), scheduler.SelectionOptions{Kind: kind, Model: model, RoutePrefix: httpbridge.Param(c, "routePrefix"), ChannelName: httpbridge.GetHeader(c, "X-Channel"), HasImageContent: profile.VisionNeed, AgentRole: profile.AgentRole, ContextRequirement: &scheduler.ContextRequirement{InputTokens: int(input), OutputTokens: int(output), ExplicitOutputMax: outputKnown}, DryRun: true})
		if err != nil || result.Upstream == nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "no_eligible_route"})
			return nil
		}
		actualModel := model
		if result.ExecutionModel != "" {
			actualModel = result.ExecutionModel
		}
		resolved := config.ResolveUpstreamCapability(actualModel, result.Upstream, cm.UpstreamModelCapabilitiesSnapshot())
		cost, complete := metrics.CalculateTokenCostUSDWithPricingAndStatus(resolved.Capability.Pricing, input, output, 0, 0)
		// Token-only quotes cannot promise full media/request charges.
		if !outputKnown || kind == scheduler.ChannelKindImages {
			complete = false
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"model": model, "resolvedModel": actualModel, "currency": "USD", "estimatedCost": cost, "pricingComplete": complete, "inputTokens": input, "outputTokens": output, "outputBudgetKnown": outputKnown, "quoteOnly": true})
		return nil
	}
}
