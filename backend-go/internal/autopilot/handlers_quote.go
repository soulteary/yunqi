package autopilot

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/middleware"
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
		if !middleware.AuthorizeProxyModel(c, model) {
			return nil
		}
		cfg := cm.GetConfig()
		profile := buildRoutePreviewProfile(c, kind, model, normalizeRoutePreviewOperation(kind, strings.TrimSpace(req.Operation), c), body, cfg.AutopilotRouting.Scenario)
		input := int64(profile.ContextNeed)
		inputKnown := quoteHasInput(body)
		inputSource := "unknown"
		if inputKnown {
			inputSource = "body_estimate"
		}
		if req.InputTokens != nil {
			input = *req.InputTokens
			inputKnown = true
			inputSource = "explicit"
		}
		output := int64(0)
		outputKnown := false
		if req.OutputTokens != nil {
			output = *req.OutputTokens
			outputKnown = true
		} else {
			for _, key := range []string{"max_output_tokens", "max_completion_tokens", "max_tokens", "generationConfig.maxOutputTokens"} {
				if value := gjson.GetBytes(body, key); value.Exists() {
					parsed, parseErr := strconv.ParseInt(value.Raw, 10, 64)
					if value.Type != gjson.Number || parseErr != nil || parsed < 0 || parsed > 1_000_000_000 {
						httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_token_count"})
						return nil
					}
					output = parsed
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
		var aclFilter scheduler.CandidateFilterFunc
		if policy := middleware.EntryPolicy(c); policy != nil && len(policy.ChannelUIDs) > 0 {
			aclFilter = func(channels []scheduler.ChannelInfo, upstreamFor func(scheduler.ChannelInfo) *config.UpstreamConfig, _ func(scheduler.ChannelInfo, *config.UpstreamConfig) bool) ([]scheduler.ChannelInfo, error) {
				filtered := make([]scheduler.ChannelInfo, 0, len(channels))
				for _, ch := range channels {
					up := upstreamFor(ch)
					if up != nil && policy.AllowsChannel(up.ChannelUID) {
						filtered = append(filtered, ch)
					}
				}
				return filtered, nil
			}
		}
		result, err := sch.SelectChannelWithOptions(ContextWithRequestProfile(httpbridge.Request(c).Context(), profile), scheduler.SelectionOptions{CandidateFilter: aclFilter, Kind: kind, Model: model, RoutePrefix: httpbridge.Param(c, "routePrefix"), ChannelName: httpbridge.GetHeader(c, "X-Channel"), HasImageContent: profile.VisionNeed, AgentRole: profile.AgentRole, ContextRequirement: &scheduler.ContextRequirement{InputTokens: int(input), OutputTokens: int(output), ExplicitOutputMax: outputKnown}, DryRun: true})
		if err != nil || result.Upstream == nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "no_eligible_route"})
			return nil
		}
		actualModel := model
		if result.ExecutionModel != "" {
			actualModel = result.ExecutionModel
		}
		if !middleware.AuthorizeProxyRoute(c, nil, actualModel, result.Upstream.ChannelUID) {
			return nil
		}
		resolved := config.ResolveUpstreamCapability(actualModel, result.Upstream, cm.UpstreamModelCapabilitiesSnapshot())
		cost, complete := metrics.CalculateTokenCostUSDWithPricingAndStatus(resolved.Capability.Pricing, input, output, 0, 0)
		// Token-only quotes cannot promise full media/request charges.
		if !inputKnown || !outputKnown || kind == scheduler.ChannelKindImages || quoteHasMedia(body) {
			complete = false
		}
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"model": model, "resolvedModel": actualModel, "currency": "USD", "estimatedCost": cost, "pricingComplete": complete, "inputTokens": input, "outputTokens": output, "outputBudgetKnown": outputKnown, "inputBudgetKnown": inputKnown, "inputBudgetSource": inputSource, "quoteOnly": true})
		return nil
	}
}

func quoteHasInput(body []byte) bool {
	for _, key := range []string{"messages", "input", "contents", "prompt", "system", "instructions"} {
		v := gjson.GetBytes(body, key)
		if v.IsArray() && len(v.Array()) > 0 || v.Type == gjson.String && v.String() != "" || v.IsObject() && len(v.Map()) > 0 {
			return true
		}
	}
	return false
}

func quoteHasMedia(body []byte) bool {
	var value interface{}
	if json.Unmarshal(body, &value) != nil {
		return false
	}
	var walk func(interface{}) bool
	walk = func(v interface{}) bool {
		switch x := v.(type) {
		case []interface{}:
			for _, item := range x {
				if walk(item) {
					return true
				}
			}
		case map[string]interface{}:
			for key, item := range x {
				switch strings.ToLower(key) {
				case "audio", "video", "image", "image_url", "input_audio", "output_audio", "inlinedata", "filedata", "modalities", "responsemodalities":
					return true
				}
				if key == "type" || strings.EqualFold(key, "mimeType") || key == "mime_type" {
					if text, ok := item.(string); ok {
						lower := strings.ToLower(text)
						for _, media := range []string{"image", "audio", "video", "document", "input_file", "application/pdf"} {
							if strings.Contains(lower, media) {
								return true
							}
						}
					}
				}
				if walk(item) {
					return true
				}
			}
		}
		return false
	}
	return walk(value)
}
