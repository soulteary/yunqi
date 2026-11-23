// Package responses 提供 Responses API 的处理器
package responses

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/providers"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/utils"
	"github.com/tidwall/sjson"
)

// Handler Responses API 代理处理器
// 支持多渠道调度：当配置多个渠道时自动启用
func Handler(
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	sessionManager *session.SessionManager,
	channelScheduler *scheduler.ChannelScheduler,
) fiber.Handler {
	return fiber.Handler(func(c fiber.Ctx) error {
		// 先进行认证
		middleware.AuthenticateProxy(c, envCfg)
		if httpbridge.IsAborted(c) {
			return nil
		}

		startTime := time.Now()

		// 读取原始请求体
		maxBodySize := envCfg.MaxRequestBodySize
		bodyBytes, err := common.ReadClientRequestBody(c, maxBodySize, cfgManager, "responses")
		if err != nil {
			return nil
		}

		// 入口保留原始请求体；按渠道在发往上游前决定是否规范化 metadata.user_id
		httpbridge.SetValue(c, "requestBodyBytes", bodyBytes)

		// 解析 Responses 请求
		var responsesReq types.ResponsesRequest
		if len(bodyBytes) > 0 {
			_ = json.Unmarshal(bodyBytes, &responsesReq)
			responsesReq.RawTools = extractRawToolsFromRequestBody(bodyBytes)
		}

		// 提取统一会话标识用于 Trace 亲和性（保持 metadata.user_id 默认规范化后的既有路由语义）
		affinityBody := common.NormalizeMetadataUserID(bodyBytes)
		userID := utils.ExtractUnifiedSessionID(c, affinityBody)
		agentCtx := utils.ExtractAgentContext(c, bodyBytes)
		httpbridge.SetValue(c, "agentContext", agentCtx)

		// 统计 user 输入用于驾驶舱标题与轮数
		lastUserMessages := extractRecentResponsesUserInputs(responsesReq.Input)
		httpbridge.SetValue(c, "lastUserMessages", lastUserMessages)
		httpbridge.SetValue(c, "lastUserMessage", strings.Join(lastUserMessages, " / "))
		httpbridge.SetValue(c, "userMessageCount", countResponsesUserMessages(responsesReq.Input))
		common.SetRequestLogContextWithAgent(c, userID, countResponsesUserMessages(responsesReq.Input), agentCtx)

		// 记录原始请求信息（仅在入口处记录一次）
		common.LogOriginalRequest(c, bodyBytes, envCfg, "Responses")

		// The optional reserved alias uses the target's existing routing. Keep the
		// original request log, instructions and approval policy intact.
		mapping := cfgManager.GetCodexAutoReviewMapping()
		if responsesReq.Model == config.CodexAutoReviewModel && mapping.Enabled && mapping.TargetModel != "" && mapping.Validate() == nil {
			mappedBody, rewriteErr := sjson.SetBytes(bodyBytes, "model", mapping.TargetModel)
			if rewriteErr != nil {
				httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "无法改写自动审批模型"})
				return nil
			}
			bodyBytes = mappedBody
			responsesReq.Model = mapping.TargetModel
			common.RestoreRequestBody(c, bodyBytes)
			httpbridge.SetValue(c, "requestBodyBytes", bodyBytes)
			common.RequestLogf(c, "[Codex-AutoReview] %s -> %s", config.CodexAutoReviewModel, mapping.TargetModel)
		}

		isCompactionV2 := hasCompactionTrigger(responsesReq.Input)
		operation := "completion"
		if isCompactionV2 {
			operation = "summarize"
		}
		common.AttachAutopilotRequestProfile(c, scheduler.ChannelKindResponses, responsesReq.Model, operation, userID, bodyBytes, 0)

		// 检查是否为多渠道模式
		isMultiChannel := channelScheduler.IsMultiChannelMode(scheduler.ChannelKindResponses)

		if isMultiChannel {
			handleMultiChannel(c, envCfg, cfgManager, channelScheduler, sessionManager, bodyBytes, responsesReq, userID, startTime, isCompactionV2)
		} else {
			handleSingleChannel(c, envCfg, cfgManager, channelScheduler, sessionManager, bodyBytes, userID, responsesReq, startTime, isCompactionV2)
		}
		return nil
	})
}

// handleMultiChannel 处理多渠道 Responses 请求
func extractRawToolsFromRequestBody(bodyBytes []byte) []interface{} {
	var reqMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &reqMap); err != nil {
		return nil
	}
	rawTools, _ := reqMap["tools"].([]interface{})
	return rawTools
}

func handleMultiChannel(
	c fiber.Ctx,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	sessionManager *session.SessionManager,
	bodyBytes []byte,
	responsesReq types.ResponsesRequest,
	userID string,
	startTime time.Time,
	isCompactionV2 bool,
) {

	cfg := cfgManager.GetConfig()
	contextRequirement := common.BuildResponsesContextRequirement(bodyBytes, cfg.ContextRouting)
	common.ApplyAgentModelProfile(contextRequirement, responsesReq.Model, cfg)
	if isCompactionV2 && contextRequirement != nil {
		contextRequirement.SkipWindowValidation = true
	}
	common.LogContextEstimate(c, "Responses", contextRequirement)
	agentRole := ""
	if ac := common.AgentContextFromFiber(c); ac != nil {
		agentRole = ac.AgentRole
	}
	common.HandleMultiChannelFailoverWithContextRequirement(
		c,
		envCfg,
		channelScheduler,
		scheduler.ChannelKindResponses,
		"Responses",
		userID,
		responsesReq.Model,
		contextRequirement,
		responsesReq.Stream,
		agentRole,
		func(c fiber.Ctx, selection *scheduler.SelectionResult) common.MultiChannelAttemptResult {
			managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

			responsesReq := responsesReq
			if responsesReq.TransformerMetadata != nil {
				metadata := make(map[string]interface{}, len(responsesReq.TransformerMetadata))
				for key, value := range responsesReq.TransformerMetadata {
					metadata[key] = value
				}
				responsesReq.TransformerMetadata = metadata
			}
			provider := &providers.ResponsesProvider{SessionManager: sessionManager}
			upstream := selection.Upstream
			executionRoute := selection.Route

			if upstream == nil {
				return common.MultiChannelAttemptResult{}
			}

			metricsManager := channelScheduler.GetMetricsManagerForRoute(executionRoute)

			if isCompactionV2 && needsLocalCompact(upstream) {
				success, successKey, compactErr := tryLocalCompactV2WithAllKeys(c, upstream, executionRoute.Index, responsesReq.Model, cfgManager, channelScheduler, channelScheduler.GetChannelLogStoreForRoute(executionRoute), bodyBytes, envCfg, sessionManager)
				result := common.MultiChannelAttemptResult{Attempted: true, SuccessKey: successKey, Route: executionRoute}
				if success {
					result.Handled = true
					return result
				}
				if compactErr != nil {
					if compactErr.shouldFailover {
						result.FailoverError = &common.FailoverError{Status: compactErr.status, Body: compactErr.body}
						result.LastError = compactErr.err
						return result
					}
					httpbridge.Data(c, compactErr.status, "application/json", compactErr.body)
					result.Handled = true
					result.LastError = compactErr.err
					return result
				}
				return result
			}

			baseURLs := upstream.GetAllBaseURLs()
			sortedURLResults := channelScheduler.GetSortedURLsForRoute(executionRoute, baseURLs)

			handled, successKey, successBaseURLIdx, failoverErr, usage, lastErr := common.TryUpstreamWithAllKeys(
				c,
				envCfg,
				cfgManager,
				channelScheduler,
				scheduler.ChannelKindResponses,
				"Responses",
				metricsManager,
				upstream,
				sortedURLResults,
				bodyBytes,
				contextRequirement,
				responsesReq.Stream,
				func(upstream *config.UpstreamConfig, failedKeys map[string]bool) (string, error) {
					return cfgManager.GetNextAPIKey(upstream, failedKeys, common.ChannelAPIType(scheduler.ChannelKind(executionRoute.Kind)))
				},
				func(c fiber.Ctx, upstreamCopy *config.UpstreamConfig, apiKey string) (*http.Request, error) {
					req, _, err := provider.ConvertToProviderRequest(c, upstreamCopy, apiKey)
					return req, err
				},
				func(apiKey string) {
					if err := cfgManager.DeprioritizeAPIKeyForRouteAudited(managementAuditContext, executionRoute.Kind, executionRoute.Index, apiKey); err != nil {
						common.RequestLogf(c, "[Responses-Key] 警告: 密钥降级失败: %v", err)
					}
				},
				func(url string) {
					channelScheduler.MarkURLFailureForRoute(executionRoute, url)
				},
				func(url string) {
					channelScheduler.MarkURLSuccessForRoute(executionRoute, url)
				},
				func(c fiber.Ctx, resp *http.Response, upstreamCopy *config.UpstreamConfig, apiKey string, actualRequestBody []byte) (*types.Usage, error) {
					// Inject codex_tool_compat_enabled for response remapping
					if responsesReq.TransformerMetadata == nil {
						responsesReq.TransformerMetadata = make(map[string]interface{})
					}
					// additional_tools 提升（默认开启，不经渠道开关）同样激活响应侧 remap
					hoisted, _ := httpbridge.Value(c, "codex_additional_tools_hoisted")
					hoistedBool, _ := hoisted.(bool)
					responsesReq.TransformerMetadata["codex_tool_compat_enabled"] = upstreamCopy.IsCodexToolCompatEnabled() || upstreamCopy.IsCodexNativeToolPassthroughEnabled() || hoistedBool
					timeouts := common.ResolveStreamPreflightTimeouts(upstreamCopy, metricsManager.GetCircuitBreakerConfig())
					return handleSuccess(c, resp, provider, upstreamCopy, apiKey, upstream.ServiceType, envCfg, sessionManager, startTime, &responsesReq, actualRequestBody, timeouts)
				},
				responsesReq.Model,
				"",
				executionRoute.Index,
				channelScheduler.GetChannelLogStoreForRoute(executionRoute),
				common.WithSelectionTrace(selection),
				common.WithExecutionRoute(executionRoute),
				common.WithExecutionModel(selection.ExecutionModel),
			)

			return common.MultiChannelAttemptResult{
				Route:             executionRoute,
				Handled:           handled,
				Attempted:         true,
				SuccessKey:        successKey,
				SuccessBaseURLIdx: successBaseURLIdx,
				FailoverError:     failoverErr,
				Usage:             usage,
				LastError:         lastErr,
			}
		},
		nil,
		func(ctx fiber.Ctx, failoverErr *common.FailoverError, lastError error) {
			common.HandleAllChannelsFailed(ctx, failoverErr, lastError, "Responses")
		},
	)
}

// handleSingleChannel 处理单渠道 Responses 请求
func handleSingleChannel(
	c fiber.Ctx,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	sessionManager *session.SessionManager,
	bodyBytes []byte,
	userID string,
	responsesReq types.ResponsesRequest,
	startTime time.Time,
	isCompactionV2 bool,
) {
	managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

	upstream, channelIndex, err := cfgManager.GetCurrentResponsesUpstreamWithIndex()
	if err != nil {
		httpbridge.JSON(c, 503, fiber.Map{
			"error": "未配置任何 Responses 渠道，请先在管理界面添加渠道",
			"code":  "NO_RESPONSES_UPSTREAM",
		})
		return
	}

	if len(upstream.APIKeys) == 0 {
		httpbridge.JSON(c, 503, fiber.Map{
			"error": fmt.Sprintf("当前 Responses 渠道 \"%s\" 未配置API密钥", upstream.Name),
			"code":  "NO_API_KEYS",
		})
		return
	}

	cfg := cfgManager.GetConfig()
	contextRequirement := common.BuildResponsesContextRequirement(bodyBytes, cfg.ContextRouting)
	common.ApplyAgentModelProfile(contextRequirement, responsesReq.Model, cfg)
	if isCompactionV2 && contextRequirement != nil {
		contextRequirement.SkipWindowValidation = true
	}
	common.LogContextEstimate(c, "Responses", contextRequirement)
	if err := channelScheduler.ValidateUpstreamContext(scheduler.ChannelKindResponses, responsesReq.Model, upstream, contextRequirement); err != nil {
		httpbridge.JSON(c, 400, fiber.Map{
			"error": err.Error(),
			"code":  "CONTEXT_WINDOW_EXCEEDED",
		})
		return
	}

	provider := &providers.ResponsesProvider{SessionManager: sessionManager}

	metricsManager := channelScheduler.GetResponsesMetricsManager()
	baseURLs := upstream.GetAllBaseURLs()

	urlResults := common.BuildDefaultURLResults(baseURLs)

	if isCompactionV2 && needsLocalCompact(upstream) {
		handled, _, compactErr := tryLocalCompactV2WithAllKeys(c, upstream, channelIndex, responsesReq.Model, cfgManager, channelScheduler, channelScheduler.GetChannelLogStore(scheduler.ChannelKindResponses), bodyBytes, envCfg, sessionManager)
		if handled {
			return
		}
		if compactErr != nil {
			httpbridge.JSON(c, 503, fiber.Map{
				"type": "error",
				"error": fiber.Map{
					"type":    "service_unavailable",
					"message": "All upstream channels are currently unavailable",
				},
			})
			return
		}
	}

	handled, successKey, _, lastFailoverError, _, lastError := common.TryUpstreamWithAllKeys(
		c,
		envCfg,
		cfgManager,
		channelScheduler,
		scheduler.ChannelKindResponses,
		"Responses",
		metricsManager,
		upstream,
		urlResults,
		bodyBytes,
		contextRequirement,
		responsesReq.Stream,
		func(upstream *config.UpstreamConfig, failedKeys map[string]bool) (string, error) {
			return cfgManager.GetNextResponsesAPIKey(upstream, failedKeys)
		},
		func(c fiber.Ctx, upstreamCopy *config.UpstreamConfig, apiKey string) (*http.Request, error) {
			req, _, err := provider.ConvertToProviderRequest(c, upstreamCopy, apiKey)
			return req, err
		},
		func(apiKey string) {
			if err := cfgManager.DeprioritizeAPIKeyAudited(managementAuditContext, apiKey); err != nil {
				common.RequestLogf(c, "[Responses-Key] 警告: 密钥降级失败: %v", err)
			}
		},
		nil,
		nil,
		func(c fiber.Ctx, resp *http.Response, upstreamCopy *config.UpstreamConfig, apiKey string, actualRequestBody []byte) (*types.Usage, error) {
			// Inject codex_tool_compat_enabled for response remapping
			if responsesReq.TransformerMetadata == nil {
				responsesReq.TransformerMetadata = make(map[string]interface{})
			}
			// additional_tools 提升（默认开启，不经渠道开关）同样激活响应侧 remap
			hoisted, _ := httpbridge.Value(c, "codex_additional_tools_hoisted")
			hoistedBool, _ := hoisted.(bool)
			responsesReq.TransformerMetadata["codex_tool_compat_enabled"] = upstreamCopy.IsCodexToolCompatEnabled() || upstreamCopy.IsCodexNativeToolPassthroughEnabled() || hoistedBool
			timeouts := common.ResolveStreamPreflightTimeouts(upstreamCopy, metricsManager.GetCircuitBreakerConfig())
			return handleSuccess(c, resp, provider, upstreamCopy, apiKey, upstream.ServiceType, envCfg, sessionManager, startTime, &responsesReq, actualRequestBody, timeouts)
		},
		responsesReq.Model,
		"",
		channelIndex,
		channelScheduler.GetChannelLogStore(scheduler.ChannelKindResponses),
	)

	// 追踪对话（驾驶舱显示）
	if handled && successKey != "" {
		lastUserMsg, _ := httpbridge.Value(c, "lastUserMessage")
		lastUserMsgStr, _ := lastUserMsg.(string)
		lastUserMsgs, _ := httpbridge.Value(c, "lastUserMessages")
		lastUserMessages, _ := lastUserMsgs.([]string)
		userMsgCount, _ := httpbridge.Value(c, "userMessageCount")
		userMsgCountInt, _ := userMsgCount.(int)

		if lastUserMsgStr != "" || userMsgCountInt > 0 {
			channelName := ""
			if upstream != nil {
				channelName = upstream.Name
			}
			agentRole := ""
			agentCtx := common.AgentContextFromFiber(c)
			if agentCtx != nil {
				agentRole = agentCtx.AgentRole
			}
			channelScheduler.TrackConversationWithMessages(
				scheduler.ChannelKindResponses,
				userID,
				responsesReq.Model,
				channelIndex,
				channelName,
				"",
				lastUserMsgStr,
				lastUserMessages,
				userMsgCountInt,
				agentRole,
				agentCtx,
			)
		}
	}

	if handled {
		return
	}

	common.RequestLogf(c, "[Responses-Error] 所有 Responses API密钥都失败了")
	common.HandleAllKeysFailed(c, lastFailoverError, lastError, "Responses")
}
