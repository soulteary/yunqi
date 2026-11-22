package images

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/errutil"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/httpclient"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/utils"
)

const (
	operationGenerations = "generations"
	operationEdits       = "edits"
	operationVariations  = "variations"

	imageURLDownloadTimeout = 30 * time.Second
	maxImageDownloadBytes   = 25 * 1024 * 1024
)

// Handler Images API 代理处理器
func Handler(
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
) fiber.Handler {
	return fiber.Handler(func(c fiber.Ctx) error {
		middleware.AuthenticateProxy(c, envCfg)
		if httpbridge.IsAborted(c) {
			return nil
		}

		operation := extractOperation(httpbridge.Request(c).URL.Path)
		if operation == "" {
			imagesErrorResponse(c, http.StatusNotFound, "Images endpoint not found", "invalid_request_error", "endpoint_not_found")
			return nil
		}

		startTime := time.Now()
		bodyBytes, err := common.ReadRequestBody(c, envCfg.MaxRequestBodySize)
		if err != nil {
			return nil
		}
		httpbridge.SetValue(c, "requestBodyBytes", bodyBytes)

		contentType := httpbridge.GetHeader(c, "Content-Type")
		model, isStream, ok := parseImagesRequest(c, bodyBytes, contentType, operation)
		if !ok {
			return nil
		}

		userID := utils.ExtractUnifiedSessionID(c, bodyBytes)
		agentCtx := utils.ExtractAgentContext(c, bodyBytes)
		httpbridge.SetValue(c, "agentContext", agentCtx)
		common.SetRequestLogContextWithAgent(c, userID, 0, agentCtx)
		common.LogOriginalRequest(c, bodyBytes, envCfg, "Images")
		common.AttachAutopilotRequestProfile(c, scheduler.ChannelKindImages, model, operation, userID, bodyBytes, 0)

		if channelScheduler.IsMultiChannelMode(scheduler.ChannelKindImages) {
			handleMultiChannel(c, envCfg, cfgManager, channelScheduler, bodyBytes, model, userID, startTime, operation, contentType, isStream)
		} else {
			handleSingleChannel(c, envCfg, cfgManager, channelScheduler, bodyBytes, model, startTime, operation, contentType, isStream)
		}
		return nil
	})
}

func extractOperation(path string) string {
	if strings.Contains(path, "/images/generations") {
		return operationGenerations
	}
	if strings.Contains(path, "/images/edits") {
		return operationEdits
	}
	if strings.Contains(path, "/images/variations") {
		return operationVariations
	}
	return ""
}

func sanitizeDiagnosticError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

func logImagesValidationFailure(c fiber.Ctx, operation string, contentType string, bodyBytes []byte, stage string, reason string, err error) {
	common.RequestLogf(c, "[Images-Validation] operation=%s method=%s path=%s content_type=%q body_bytes=%d stage=%s reason=%s error=%q",
		operation, httpbridge.Request(c).Method, httpbridge.Request(c).URL.Path, contentType,
		len(bodyBytes),
		stage,
		reason,
		sanitizeDiagnosticError(err),
	)
}

func logImagesMultipartFailure(c fiber.Ctx, operation string, contentType string, bodyBytes []byte, err error) {
	stage, reason := describeMultipartDiagnostic(err)
	common.RequestLogf(c, "[Images-Multipart] operation=%s method=%s path=%s content_type=%q body_bytes=%d stage=%s reason=%s multipart=true boundary_present=%t error=%q",
		operation, httpbridge.Request(c).Method, httpbridge.Request(c).URL.Path, contentType,
		len(bodyBytes),
		stage,
		reason,
		hasMultipartBoundary(contentType),
		sanitizeDiagnosticError(err),
	)
}

func logImagesBuildRequestFailure(c fiber.Ctx, operation string, baseURL string, apiKey string, model string, contentType string, bodyBytes []byte, stage string, reason string, err error) {
	common.RequestLogf(c, "[Images-BuildRequest] operation=%s base_url=%q key_mask=%s model=%q content_type=%q body_bytes=%d stage=%s reason=%s error=%q",
		operation,
		baseURL,
		utils.MaskAPIKey(apiKey),
		model,
		contentType,
		len(bodyBytes),
		stage,
		reason,
		sanitizeDiagnosticError(err),
	)
}

func parseImagesRequest(c fiber.Ctx, bodyBytes []byte, contentType string, operation string) (string, bool, bool) {
	if operation != operationGenerations {
		if isJSONContentType(contentType) {
			var reqMap map[string]interface{}
			if len(bodyBytes) > 0 {
				if err := json.Unmarshal(bodyBytes, &reqMap); err != nil {
					logImagesValidationFailure(c, operation, contentType, bodyBytes, "parse_json", "invalid_json", err)
					imagesErrorResponse(c, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err), "invalid_request_error", "invalid_json")
					return "", false, false
				}
			}
			model, _ := reqMap["model"].(string)
			if strings.TrimSpace(model) == "" {
				model = "gpt-image-2"
			}
			return model, isImagesStreamRequest(c, bodyBytes, contentType), true
		}
		if isMultipartContentType(contentType) {
			if err := validateMultipartBody(bodyBytes, contentType); err != nil {
				logImagesMultipartFailure(c, operation, contentType, bodyBytes, err)
				imagesErrorResponse(c, http.StatusBadRequest, fmt.Sprintf("Invalid multipart body: %v", err), "invalid_request_error", "invalid_multipart")
				return "", false, false
			}
		} else {
			logImagesValidationFailure(c, operation, contentType, bodyBytes, "content_type", "invalid_content_type", fmt.Errorf("unsupported content type for images %s", operation))
		}
		model := extractImagesModel(bodyBytes, contentType)
		if strings.TrimSpace(model) == "" {
			model = "gpt-image-2"
		}
		return model, isImagesStreamRequest(c, bodyBytes, contentType), true
	}

	var reqMap map[string]interface{}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &reqMap); err != nil {
			logImagesValidationFailure(c, operation, contentType, bodyBytes, "parse_json", "invalid_json", err)
			imagesErrorResponse(c, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err), "invalid_request_error", "invalid_json")
			return "", false, false
		}
	}

	model, _ := reqMap["model"].(string)
	if model == "" {
		logImagesValidationFailure(c, operation, contentType, bodyBytes, "validate_required", "missing_model", fmt.Errorf("model is required"))
		imagesErrorResponse(c, http.StatusBadRequest, "model is required", "invalid_request_error", "missing_parameter")
		return "", false, false
	}
	prompt, _ := reqMap["prompt"].(string)
	if prompt == "" {
		logImagesValidationFailure(c, operation, contentType, bodyBytes, "validate_required", "missing_prompt", fmt.Errorf("prompt is required"))
		imagesErrorResponse(c, http.StatusBadRequest, "prompt is required", "invalid_request_error", "missing_parameter")
		return "", false, false
	}

	return model, isImagesStreamRequest(c, bodyBytes, contentType), true
}

func handleMultiChannel(
	c fiber.Ctx,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	bodyBytes []byte,
	model string,
	userID string,
	startTime time.Time,
	operation string,
	contentType string,
	isStream bool,
) {
	metricsManager := channelScheduler.GetImagesMetricsManager()
	agentRole := ""
	if ac := common.AgentContextFromFiber(c); ac != nil {
		agentRole = ac.AgentRole
	}
	common.HandleMultiChannelFailover(
		c,
		envCfg,
		channelScheduler,
		scheduler.ChannelKindImages,
		"Images",
		userID,
		model,
		false,
		agentRole,
		func(c fiber.Ctx, selection *scheduler.SelectionResult) common.MultiChannelAttemptResult {
			managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

			upstream := selection.Upstream
			channelIndex := selection.ChannelIndex
			if upstream == nil {
				return common.MultiChannelAttemptResult{}
			}

			baseURLs := upstream.GetAllBaseURLs()
			sortedURLResults := channelScheduler.GetSortedURLsForChannel(scheduler.ChannelKindImages, channelIndex, baseURLs)
			handled, successKey, successBaseURLIdx, failoverErr, usage, lastErr := common.TryUpstreamWithAllKeys(
				c,
				envCfg,
				cfgManager,
				channelScheduler,
				scheduler.ChannelKindImages,
				"Images",
				metricsManager,
				upstream,
				sortedURLResults,
				bodyBytes,
				nil,
				isStream,
				func(upstream *config.UpstreamConfig, failedKeys map[string]bool) (string, error) {
					return cfgManager.GetNextImagesAPIKey(upstream, failedKeys)
				},
				func(c fiber.Ctx, upstreamCopy *config.UpstreamConfig, apiKey string) (*http.Request, error) {
					return buildOperationRequest(c, upstreamCopy, upstreamCopy.BaseURL, apiKey, bodyBytes, model, operation, contentType)
				},
				func(apiKey string) {
					_ = cfgManager.DeprioritizeAPIKeyAudited(managementAuditContext, apiKey)
				},
				func(url string) {
					channelScheduler.MarkURLFailure(scheduler.ChannelKindImages, channelIndex, url)
				},
				func(url string) {
					channelScheduler.MarkURLSuccess(scheduler.ChannelKindImages, channelIndex, url)
				},
				func(c fiber.Ctx, resp *http.Response, upstreamCopy *config.UpstreamConfig, apiKey string, actualRequestBody []byte) (*types.Usage, error) {
					return handleSuccess(c, resp, envCfg, startTime, isStream, common.ResolveStreamPreflightTimeouts(upstreamCopy, metricsManager.GetCircuitBreakerConfig()), upstreamCopy, actualRequestBody, contentType)
				},
				model,
				operation,
				selection.ChannelIndex,
				channelScheduler.GetChannelLogStore(scheduler.ChannelKindImages),
				common.WithSelectionTrace(selection),
			)

			return common.MultiChannelAttemptResult{
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
			handleAllChannelsFailed(ctx, failoverErr, lastError)
		},
	)
}

func handleSingleChannel(
	c fiber.Ctx,
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	channelScheduler *scheduler.ChannelScheduler,
	bodyBytes []byte,
	model string,
	startTime time.Time,
	operation string,
	contentType string,
	isStream bool,
) {
	managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

	upstream, channelIndex, err := cfgManager.GetCurrentImagesUpstreamWithIndex()
	if err != nil {
		imagesErrorResponse(c, http.StatusServiceUnavailable, "No Images upstream configured", "service_unavailable", "service_unavailable")
		return
	}
	if len(upstream.APIKeys) == 0 {
		imagesErrorResponse(c, http.StatusServiceUnavailable, fmt.Sprintf("No API keys configured for upstream \"%s\"", upstream.Name), "service_unavailable", "service_unavailable")
		return
	}

	metricsManager := channelScheduler.GetImagesMetricsManager()
	baseURLs := upstream.GetAllBaseURLs()
	urlResults := common.BuildDefaultURLResults(baseURLs)
	handled, _, _, lastFailoverError, _, lastError := common.TryUpstreamWithAllKeys(
		c,
		envCfg,
		cfgManager,
		channelScheduler,
		scheduler.ChannelKindImages,
		"Images",
		metricsManager,
		upstream,
		urlResults,
		bodyBytes,
		nil,
		isStream,
		func(upstream *config.UpstreamConfig, failedKeys map[string]bool) (string, error) {
			return cfgManager.GetNextImagesAPIKey(upstream, failedKeys)
		},
		func(c fiber.Ctx, upstreamCopy *config.UpstreamConfig, apiKey string) (*http.Request, error) {
			return buildOperationRequest(c, upstreamCopy, upstreamCopy.BaseURL, apiKey, bodyBytes, model, operation, contentType)
		},
		func(apiKey string) {
			_ = cfgManager.DeprioritizeAPIKeyAudited(managementAuditContext, apiKey)
		},
		nil,
		nil,
		func(c fiber.Ctx, resp *http.Response, upstreamCopy *config.UpstreamConfig, apiKey string, actualRequestBody []byte) (*types.Usage, error) {
			return handleSuccess(c, resp, envCfg, startTime, isStream, common.ResolveStreamPreflightTimeouts(upstreamCopy, metricsManager.GetCircuitBreakerConfig()), upstreamCopy, actualRequestBody, contentType)
		},
		model,
		operation,
		channelIndex,
		channelScheduler.GetChannelLogStore(scheduler.ChannelKindImages),
	)
	if handled {
		return
	}

	common.RequestLogf(c, "[Images-Error] 所有 API密钥都失败了")
	handleAllKeysFailed(c, lastFailoverError, lastError)
}

func buildProviderRequest(
	c fiber.Ctx,
	upstream *config.UpstreamConfig,
	baseURL string,
	apiKey string,
	bodyBytes []byte,
	model string,
) (*http.Request, error) {
	return buildOperationRequest(c, upstream, baseURL, apiKey, bodyBytes, model, operationGenerations, "application/json")
}

func buildOperationRequest(
	c fiber.Ctx,
	upstream *config.UpstreamConfig,
	baseURL string,
	apiKey string,
	bodyBytes []byte,
	model string,
	operation string,
	contentType string,
) (*http.Request, error) {
	serviceType, err := config.NormalizeImagesServiceTypeForProxy(upstream.ServiceType)
	if err != nil {
		logImagesBuildRequestFailure(c, operation, baseURL, apiKey, model, contentType, bodyBytes, "normalize_service_type", "invalid_service_type", err)
		return nil, err
	}
	upstream.ServiceType = serviceType

	requestBody := bodyBytes
	requestContentType := contentType
	redirectedModel := model

	if isMultipartContentType(contentType) {
		originalModel, hasModelField := extractMultipartField(bodyBytes, contentType, "model")
		if redirectedModel != "" && (!hasModelField || strings.TrimSpace(originalModel) == "" || redirectedModel != originalModel) {
			requestBody, requestContentType, err = rewriteMultipartFormField(bodyBytes, contentType, "model", redirectedModel)
			if err != nil {
				stage, reason := describeMultipartDiagnostic(err)
				if stage == "" {
					stage = "rewrite_field"
				}
				if reason == "" {
					reason = "part_read_failed"
				}
				logImagesBuildRequestFailure(c, operation, baseURL, apiKey, redirectedModel, contentType, bodyBytes, stage, reason, err)
				return nil, err
			}
		}
	} else if operation == operationGenerations || len(bodyBytes) > 0 {
		requestBody, requestContentType, err = buildJSONRequestBody(bodyBytes, model, redirectedModel, operation)
		if err != nil {
			logImagesBuildRequestFailure(c, operation, baseURL, apiKey, redirectedModel, contentType, bodyBytes, "build_json", "invalid_json", err)
			return nil, err
		}
	}

	url := buildOperationURL(baseURL, operation)
	req, err := http.NewRequestWithContext(httpbridge.Request(c).Context(), http.MethodPost, url, bytes.NewReader(requestBody))
	if err != nil {
		logImagesBuildRequestFailure(c, operation, baseURL, apiKey, redirectedModel, requestContentType, requestBody, "new_request", "request_init_failed", err)
		return nil, err
	}
	if httpbridge.Request(c).URL != nil {
		req.URL.RawQuery = httpbridge.Request(c).URL.RawQuery
	}
	req.Header = prepareImagesUpstreamHeaders(c, req.URL.Host, requestContentType)
	utils.SetAuthenticationHeader(req.Header, apiKey)
	utils.ApplyCustomHeaders(req.Header, upstream.CustomHeaders)
	return req, nil
}

func buildOperationURL(baseURL string, operation string) string {
	skipVersionPrefix := strings.HasSuffix(baseURL, "#")
	baseURL = strings.TrimSuffix(baseURL, "#")
	baseURL = strings.TrimRight(baseURL, "/")
	if skipVersionPrefix {
		return fmt.Sprintf("%s/images/%s", baseURL, operation)
	}
	return fmt.Sprintf("%s/v1/images/%s", baseURL, operation)
}

func buildJSONRequestBody(bodyBytes []byte, model string, redirectedModel string, operation string) ([]byte, string, error) {
	var reqMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &reqMap); err != nil {
		if operation == operationGenerations {
			return nil, "", err
		}
		return bodyBytes, "application/json", nil
	}
	if reqMap == nil {
		reqMap = make(map[string]interface{})
	}
	if model != "" && redirectedModel != "" {
		reqMap["model"] = redirectedModel
	}
	requestBody, err := json.Marshal(reqMap)
	if err != nil {
		return nil, "", err
	}
	return requestBody, "application/json", nil
}

func prepareImagesUpstreamHeaders(c fiber.Ctx, targetHost string, contentType string) http.Header {
	headers := httpbridge.Request(c).Header.Clone()
	headers.Set("Host", targetHost)
	headers.Del("x-proxy-key")
	headers.Del("X-Forwarded-For")
	headers.Del("X-Forwarded-Host")
	headers.Del("X-Forwarded-Proto")
	headers.Del("X-Real-IP")
	headers.Del("Via")
	headers.Del("Forwarded")
	headers.Del("Accept-Encoding")
	// 网关内部路由语义头，不透传上游
	headers.Del("X-Task-Domain")
	headers.Del("X-Routing-Scenario")
	headers.Del("X-Cost-Preference")
	if strings.TrimSpace(contentType) == "" {
		headers.Del("Content-Type")
	} else {
		headers.Set("Content-Type", contentType)
	}
	return headers
}

func preflightImagesStream(resp *http.Response, timeouts common.StreamPreflightTimeouts, observers ...*common.StreamTimeoutObserver) ([]byte, <-chan []byte, <-chan error, error) {
	chunkChan, bodyErrChan := common.StartBodyChunkReader(resp.Body, 4*1024, 16, common.RequestDone(resp))
	var buffered bytes.Buffer
	hasFirstContent := false
	var observer *common.StreamTimeoutObserver
	if len(observers) > 0 {
		observer = observers[0]
	}

	var firstContentTimer *time.Timer
	firstContentChan := (<-chan time.Time)(nil)
	if timeouts.FirstContentTimeoutMs > 0 {
		firstContentTimer = time.NewTimer(time.Duration(timeouts.FirstContentTimeoutMs) * time.Millisecond)
		firstContentChan = firstContentTimer.C
		defer firstContentTimer.Stop()
	}

	var inactivityTimer *time.Timer
	inactivityChan := (<-chan time.Time)(nil)

	for {
		select {
		case chunk, ok := <-chunkChan:
			if !ok {
				if buffered.Len() == 0 {
					return nil, chunkChan, bodyErrChan, common.ErrEmptyStreamResponse
				}
				return buffered.Bytes(), chunkChan, bodyErrChan, nil
			}
			if len(chunk) == 0 {
				continue
			}
			buffered.Write(chunk)
			if !hasFirstContent {
				if observer != nil {
					observer.MarkFirstContent(time.Now())
				}
				hasFirstContent = true
				if firstContentTimer != nil {
					firstContentTimer.Stop()
				}
				if timeouts.InactivityTimeoutMs > 0 {
					inactivityTimer = time.NewTimer(time.Duration(timeouts.InactivityTimeoutMs) * time.Millisecond)
					inactivityChan = inactivityTimer.C
					defer inactivityTimer.Stop()
				} else {
					return buffered.Bytes(), chunkChan, bodyErrChan, nil
				}
				continue
			}
			if observer != nil {
				observer.MarkStreamActivity(time.Now())
			}
			return buffered.Bytes(), chunkChan, bodyErrChan, nil
		case err := <-bodyErrChan:
			return nil, chunkChan, bodyErrChan, err
		case <-firstContentChan:
			return nil, chunkChan, bodyErrChan, common.ErrStreamFirstContentTimeout
		case <-inactivityChan:
			return nil, chunkChan, bodyErrChan, common.ErrStreamStalled
		}
	}
}

func handleSuccess(c fiber.Ctx, resp *http.Response, envCfg *config.EnvConfig, startTime time.Time, isStream bool, timeouts common.StreamPreflightTimeouts, upstream *config.UpstreamConfig, requestBody []byte, requestContentType string) (*types.Usage, error) {
	defer errutil.IgnoreDeferred(resp.Body.Close)
	if isStream {
		return nil, passthroughStreamingResponseWithLog(c, resp, envCfg, startTime, timeouts)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		imagesErrorResponse(c, http.StatusInternalServerError, "Failed to read response", "server_error", "server_error")
		return nil, err
	}
	if envCfg.EnableResponseLogs {
		responseTime := time.Since(startTime).Milliseconds()
		common.RequestLogf(c, "[Images-Timing] 响应完成: %dms, 状态: %d", responseTime, resp.StatusCode)
		common.LogUpstreamResponse(c, resp, bodyBytes, envCfg, "Images")
	}
	var respMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &respMap); err == nil {
		if shouldConvertImageURLToB64JSON(upstream, requestBody, requestContentType) {
			convertedBody, err := convertImageURLResponseToB64JSON(httpbridge.Request(c).Context(), respMap, upstream)
			if err != nil {
				return nil, fmt.Errorf("%w: image url to b64_json conversion failed: %v", common.ErrInvalidResponseBody, err)
			}
			bodyBytes = convertedBody
			resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(bodyBytes)))
		}
	} else {
		respMap = nil
	}
	utils.ForwardResponseHeaders(resp.Header, httpbridge.Writer(c))
	utils.ForwardContentType(resp.Header, httpbridge.Writer(c))
	httpbridge.Status(c, resp.StatusCode)
	if _, err := httpbridge.Writer(c).Write(bodyBytes); err != nil {
		return nil, err
	}
	if respMap == nil {
		return nil, nil
	}
	usage := &types.Usage{}
	if u, ok := respMap["usage"].(map[string]interface{}); ok {
		input, _ := u["input_tokens"].(float64)
		output, _ := u["output_tokens"].(float64)
		usage.InputTokens = int(input)
		usage.OutputTokens = int(output)
		types.CaptureUsageSource(usage, "images", bodyBytes)
	}
	imageCount := 0
	if data, ok := respMap["data"].([]interface{}); ok {
		for _, item := range data {
			if image, ok := item.(map[string]interface{}); ok {
				url, _ := image["url"].(string)
				b64, _ := image["b64_json"].(string)
				if url != "" || b64 != "" {
					imageCount++
				}
			}
		}
	}
	if imageCount > 0 {
		quality, size := "", ""
		if isMultipartContentType(requestContentType) {
			quality, _ = extractMultipartField(requestBody, requestContentType, "quality")
			size, _ = extractMultipartField(requestBody, requestContentType, "size")
		} else {
			var attrs struct {
				Quality string `json:"quality"`
				Size    string `json:"size"`
			}
			_ = json.Unmarshal(requestBody, &attrs)
			quality, size = attrs.Quality, attrs.Size
		}
		usage.ImageUnits = []types.ImageUsage{{Count: imageCount, Quality: quality, Size: size}}
	}
	if usage.Provenance == nil && imageCount > 0 {
		raw, _ := json.Marshal(map[string]int{"image_count": imageCount})
		usage.Provenance = &types.UsageProvenance{UsageSource: "provider", Protocol: "images", RawUsage: raw}
	}
	if usage.Provenance == nil && usage.InputTokens == 0 && usage.OutputTokens == 0 && imageCount == 0 {
		return nil, nil
	}
	return usage, nil
}

func shouldConvertImageURLToB64JSON(upstream *config.UpstreamConfig, requestBody []byte, contentType string) bool {
	if upstream == nil || !upstream.ConvertImageURLToB64JSON {
		return false
	}
	if isMultipartContentType(contentType) {
		responseFormat, ok := extractMultipartField(requestBody, contentType, "response_format")
		return ok && strings.EqualFold(strings.TrimSpace(responseFormat), "b64_json")
	}
	var reqMap map[string]interface{}
	if err := json.Unmarshal(requestBody, &reqMap); err != nil {
		return false
	}
	responseFormat, _ := reqMap["response_format"].(string)
	return strings.EqualFold(strings.TrimSpace(responseFormat), "b64_json")
}

func convertImageURLResponseToB64JSON(ctx context.Context, respMap map[string]interface{}, upstream *config.UpstreamConfig) ([]byte, error) {
	data, ok := respMap["data"].([]interface{})
	if !ok {
		return json.Marshal(respMap)
	}
	for _, rawItem := range data {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}
		if b64, _ := item["b64_json"].(string); strings.TrimSpace(b64) != "" {
			continue
		}
		imageURL, _ := item["url"].(string)
		if strings.TrimSpace(imageURL) == "" {
			continue
		}
		b64, err := downloadImageAsBase64(ctx, imageURL, upstream)
		if err != nil {
			return nil, err
		}
		item["b64_json"] = b64
		delete(item, "url")
	}
	return json.Marshal(respMap)
}

func downloadImageAsBase64(ctx context.Context, rawURL string, upstream *config.UpstreamConfig) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid image url: %w", err)
	}
	if parsedURL.Scheme == "data" {
		if idx := strings.Index(rawURL, ","); idx >= 0 {
			meta := rawURL[:idx]
			dataPart := rawURL[idx+1:]
			if strings.Contains(meta, ";base64") {
				decoded, decodeErr := base64.StdEncoding.DecodeString(dataPart)
				if decodeErr != nil {
					return "", decodeErr
				}
				return base64.StdEncoding.EncodeToString(decoded), nil
			}
		}
		return "", fmt.Errorf("unsupported data image url")
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("unsupported image url scheme: %s", parsedURL.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "image/*")

	insecureSkipVerify := false
	proxyURL := ""
	if upstream != nil {
		insecureSkipVerify = upstream.InsecureSkipVerify
		proxyURL = upstream.ProxyURL
	}
	opts := httpclient.ClientOptions{Timeout: imageURLDownloadTimeout, Insecure: insecureSkipVerify, ProxyURL: proxyURL}
	if upstream != nil {
		opts.OutboundNetwork = upstream.OutboundNetwork
		opts.ProxyPreferDirect = upstream.ProxyPreferDirect
	}
	client := httpclient.GetManager().NewClient(opts)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer errutil.IgnoreDeferred(resp.Body.Close)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("image download failed with status %d", resp.StatusCode)
	}
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if contentType != "" && !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("image download returned non-image content type %q", contentType)
	}

	limitedReader := io.LimitReader(resp.Body, maxImageDownloadBytes+1)
	imageBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", err
	}
	if len(imageBytes) > maxImageDownloadBytes {
		return "", fmt.Errorf("image download exceeds %d bytes", maxImageDownloadBytes)
	}
	if len(imageBytes) == 0 {
		return "", fmt.Errorf("image download returned empty body")
	}
	if contentType == "" {
		detectedContentType := strings.ToLower(http.DetectContentType(imageBytes))
		if !strings.HasPrefix(detectedContentType, "image/") {
			return "", fmt.Errorf("image download returned non-image body %q", detectedContentType)
		}
	}
	return base64.StdEncoding.EncodeToString(imageBytes), nil
}

func passthroughStreamingResponse(c fiber.Ctx, resp *http.Response) error {
	return passthroughStreamingResponseWithLog(c, resp, config.NewEnvConfig(), time.Now(), common.StreamPreflightTimeouts{})
}

func passthroughStreamingResponseWithLog(c fiber.Ctx, resp *http.Response, envCfg *config.EnvConfig, startTime time.Time, timeouts common.StreamPreflightTimeouts) error {
	if envCfg.EnableResponseLogs {
		responseTime := time.Since(startTime).Milliseconds()
		common.RequestLogf(c, "[Images-Stream] 流式响应开始: %dms, 状态: %d", responseTime, resp.StatusCode)
		common.LogUpstreamResponseHeaders(c, resp, envCfg, "Images")
	}

	bufferedBytes, chunkChan, bodyErrChan, err := preflightImagesStream(resp, timeouts, common.GetStreamTimeoutObserver(c))
	if err != nil {
		switch err {
		case common.ErrStreamFirstContentTimeout:
			common.RequestLogf(c, "[Images-FirstContentTimeout] 流式首块超时: %dms，触发重试", timeouts.FirstContentTimeoutMs)
		case common.ErrStreamStalled:
			common.RequestLogf(c, "[Images-StreamStalled] 流式断流: 首块后 %dms 无活动，触发重试", timeouts.InactivityTimeoutMs)
		}
		return err
	}

	utils.ForwardResponseHeaders(resp.Header, httpbridge.Writer(c))
	utils.ForwardContentType(resp.Header, httpbridge.Writer(c))
	httpbridge.Status(c, resp.StatusCode)

	flusher, ok := httpbridge.Writer(c).(http.Flusher)
	if !ok {
		bodyBytes, err := io.ReadAll(resp.Body)
		if len(bodyBytes) > 0 {
			common.LogUpstreamResponseBody(c, bodyBytes, envCfg, "Images")
			if _, writeErr := httpbridge.Writer(c).Write(bodyBytes); writeErr != nil {
				err = writeErr
			}
		}
		if envCfg.EnableResponseLogs {
			responseTime := time.Since(startTime).Milliseconds()
			common.RequestLogf(c, "[Images-Stream] 流式响应完成: %dms", responseTime)
		}
		return err
	}

	progress := common.NewStreamProgressLogger("Images", startTime, envCfg.ShouldLog("info"), common.RequestLogTag(c)).WithMetricsContext(httpbridge.Request(c).Context())
	defer progress.FinishMetricsFallback()

	// 回放缓冲的首个 chunk
	if len(bufferedBytes) > 0 {
		progress.AddBytes(len(bufferedBytes))
		progress.Tick()
		if _, writeErr := httpbridge.Writer(c).Write(bufferedBytes); writeErr != nil {
			if common.IsClientDisconnectError(writeErr) || writeErr == io.ErrClosedPipe || strings.Contains(strings.ToLower(writeErr.Error()), "closed pipe") {
				return context.Canceled
			}
			return writeErr
		}
		flusher.Flush()
	}

	// post-commit：Header 已发送后的 chunk 活动 watchdog，Images 无语义结构，任何有效 chunk 均视为活动
	logBuffer := common.NewLimitedLogBuffer(common.MaxUpstreamResponseLogBytes)
	streamLoggingEnabled := envCfg.ShouldLogResponseBodies()
	var postCommitTimer *time.Timer
	var postCommitChan <-chan time.Time
	if timeouts.InactivityTimeoutMs > 0 {
		postCommitTimer = time.NewTimer(time.Duration(timeouts.InactivityTimeoutMs) * time.Millisecond)
		postCommitChan = postCommitTimer.C
		defer postCommitTimer.Stop()
	}

	for {
		select {
		case chunk, ok := <-chunkChan:
			if !ok {
				goto streamEnd
			}
			if len(chunk) > 0 {
				common.MarkStreamActivity(c)
				progress.AddBytes(len(chunk))
				progress.Tick()
				if streamLoggingEnabled {
					_, _ = logBuffer.Write(chunk)
				}
				if _, writeErr := httpbridge.Writer(c).Write(chunk); writeErr != nil {
					if common.IsClientDisconnectError(writeErr) || writeErr == io.ErrClosedPipe || strings.Contains(strings.ToLower(writeErr.Error()), "closed pipe") {
						return context.Canceled
					}
					return writeErr
				}
				flusher.Flush()
			}
			if postCommitTimer != nil {
				if !postCommitTimer.Stop() {
					select {
					case <-postCommitTimer.C:
					default:
					}
				}
				postCommitTimer.Reset(time.Duration(timeouts.InactivityTimeoutMs) * time.Millisecond)
			}
		case bodyErr, ok := <-bodyErrChan:
			if ok && bodyErr != nil {
				return bodyErr
			}
			goto streamEnd
		case <-postCommitChan:
			progress.Finish("stalled")
			common.RequestLogf(c, "[Images-StreamStalled] 流式断流: 首字后 %dms 无上游 chunk 到达", timeouts.InactivityTimeoutMs)
			return common.ErrStreamPostCommitStalled
		}
	}
streamEnd:
	progress.Finish("completed")

	if logBuffer.Len() > 0 {
		common.LogUpstreamResponseBody(c, logBuffer.Bytes(), envCfg, "Images")
	}
	if envCfg.EnableResponseLogs {
		responseTime := time.Since(startTime).Milliseconds()
		common.RequestLogf(c, "[Images-Stream] 流式响应完成: %dms", responseTime)
	}
	return nil
}

func imagesErrorResponse(c fiber.Ctx, statusCode int, message, errorType, code string) {
	httpbridge.JSON(c, statusCode, fiber.Map{
		"error": fiber.Map{
			"message": message,
			"type":    errorType,
			"code":    code,
		},
	})
}

func handleAllChannelsFailed(c fiber.Ctx, failoverErr *common.FailoverError, lastError error) {
	if common.HandleEntryBudgetError(c, lastError) {
		return
	}
	if failoverErr != nil {
		httpbridge.Data(c, failoverErr.Status, "application/json", failoverErr.Body)
		return
	}
	errMsg := "All channels failed"
	if lastError != nil {
		errMsg = lastError.Error()
	}
	imagesErrorResponse(c, http.StatusServiceUnavailable, errMsg, "service_unavailable", "service_unavailable")
}

func handleAllKeysFailed(c fiber.Ctx, failoverErr *common.FailoverError, lastError error) {
	if common.HandleEntryBudgetError(c, lastError) {
		return
	}
	if failoverErr != nil {
		httpbridge.Data(c, failoverErr.Status, "application/json", failoverErr.Body)
		return
	}
	errMsg := "All API keys failed"
	if lastError != nil {
		errMsg = lastError.Error()
	}
	imagesErrorResponse(c, http.StatusServiceUnavailable, errMsg, "service_unavailable", "service_unavailable")
}
