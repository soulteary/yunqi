package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/handlers"
	"github.com/soulteary/yunqi/internal/handlers/chat"
	"github.com/soulteary/yunqi/internal/handlers/gemini"
	"github.com/soulteary/yunqi/internal/handlers/messages"
	"github.com/soulteary/yunqi/internal/handlers/responses"
	"github.com/soulteary/yunqi/internal/healthcheck"
	"github.com/soulteary/yunqi/internal/upstreamprobe"
	"github.com/gin-gonic/gin"
)

func buildChannelDiscoveryModelFetchers(cfgManager *config.ConfigManager) handlers.ChannelDiscoveryModelFetchers {
	return handlers.ChannelDiscoveryModelFetchers{
		"messages":  channelModelsHandlerFetcher(messages.GetChannelModels(cfgManager)),
		"responses": channelModelsHandlerFetcher(responses.GetChannelModels(cfgManager)),
		"chat":      channelModelsHandlerFetcher(chat.GetChannelModels(cfgManager)),
		"gemini":    channelModelsHandlerFetcher(gemini.GetChannelModels(cfgManager)),
	}
}

func channelModelsHandlerFetcher(handler gin.HandlerFunc) handlers.ChannelDiscoveryModelFetcher {
	return func(ctx context.Context, req handlers.DiscoveryModelsFetchRequest) (handlers.DiscoveryModelsFetchResponse, error) {
		body, err := json.Marshal(map[string]any{
			"key":                      req.APIKey,
			"baseUrl":                  req.BaseURL,
			"baseUrls":                 req.BaseURLs,
			"serviceType":              req.ServiceType,
			"proxyUrl":                 req.ProxyURL,
			"proxyPreferDirect":        req.ProxyPreferDirect,
			"insecureSkipVerify":       req.InsecureSkipVerify,
			"customHeaders":            req.CustomHeaders,
			"authHeader":               req.AuthHeader,
			"learnedClientFingerprint": req.LearnedClientFingerprint,
		})
		if err != nil {
			return handlers.DiscoveryModelsFetchResponse{}, err
		}

		recorder := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(recorder)
		httpReq := httptest.NewRequest(http.MethodPost, "/internal/channel-discovery/models", bytes.NewReader(body)).WithContext(ctx)
		httpReq.Header.Set("Content-Type", "application/json")
		ginCtx.Request = httpReq
		ginCtx.Params = gin.Params{{Key: "id", Value: "0"}}

		handler(ginCtx)

		return handlers.DiscoveryModelsFetchResponse{
			StatusCode: recorder.Code,
			Body:       append([]byte(nil), recorder.Body.Bytes()...),
		}, nil
	}
}

// healthCheckL1Fetcher 将渠道 GetChannelModels handler 适配为保活验证的 L1Fetcher
// （与 channelModelsHandlerFetcher 同一包装路径，响应归一化在 healthcheck 包内完成）。
// 火山 Agent/Coding Plan 官方入口不走通用 /v1/models（套餐 Key 无法通过该接口探测），
// 改用共享数据面探针 + 内置 manifest 模型清单，并标记 RealCallVerified 跳过同周期等价 L2。
func healthCheckL1Fetcher(handler gin.HandlerFunc) healthcheck.L1Fetcher {
	fetcher := channelModelsHandlerFetcher(handler)
	return func(ctx context.Context, req healthcheck.L1Request) (healthcheck.L1Response, error) {
		if upstreamprobe.IsVolcenginePlanBaseURL(req.BaseURL) {
			// 使用内置 manifest 模型清单作为候选，让 L1 探针动态选择最便宜可用模型
			candidates := volcenginePlanCandidates(req.BaseURL, req.ServiceType)
			sc, body, model, err := upstreamprobe.VolcenginePlanL1Probe(ctx, req.ServiceType, req.BaseURL, req.APIKey, req.AuthHeader, candidates, upstreamprobe.ProbeOptions{
				ProxyURL:           req.ProxyURL,
				ProxyPreferDirect:  req.ProxyPreferDirect,
				CustomHeaders:      req.CustomHeaders,
				InsecureSkipVerify: req.InsecureSkipVerify,
			})
			if err != nil {
				return healthcheck.L1Response{RealCallVerified: true, Model: model}, err
			}
			return healthcheck.L1Response{StatusCode: sc, Body: body, RealCallVerified: true, Model: model}, nil
		}
		resp, err := fetcher(ctx, handlers.DiscoveryModelsFetchRequest{
			ServiceType:              req.ServiceType,
			BaseURL:                  req.BaseURL,
			APIKey:                   req.APIKey,
			AuthHeader:               req.AuthHeader,
			CustomHeaders:            req.CustomHeaders,
			ProxyURL:                 req.ProxyURL,
			ProxyPreferDirect:        req.ProxyPreferDirect,
			InsecureSkipVerify:       req.InsecureSkipVerify,
			LearnedClientFingerprint: req.LearnedClientFingerprint,
		})
		if err != nil {
			return healthcheck.L1Response{}, err
		}
		return healthcheck.L1Response{StatusCode: resp.StatusCode, Body: resp.Body}, nil
	}
}

// volcenginePlanCandidates 返回火山套餐入口对应的内置候选模型清单。
// 用于 healthcheck L1 动态选择探针模型；无 manifest 时返回 nil，由上游探针回退常量。
func volcenginePlanCandidates(baseURL, serviceType string) []string {
	manifest, ok := config.LookupBuiltinManifest(baseURL, volcengineManifestServiceType(serviceType))
	if !ok {
		return nil
	}
	return manifest.ModelIDs
}

// volcengineManifestServiceType 把 healthcheck serviceType 归一化为 manifest 查找口径。
func volcengineManifestServiceType(serviceType string) string {
	switch strings.ToLower(strings.TrimSpace(serviceType)) {
	case "claude", "messages":
		return "messages"
	case "openai":
		return "openai"
	default:
		return strings.ToLower(strings.TrimSpace(serviceType))
	}
}

// healthCheckAPIType 保活验证渠道类型到 ConfigManager.BlacklistKeyWithRecoverAt apiType 的映射
func healthCheckAPIType(channelType string) string {
	switch channelType {
	case "messages":
		return "Messages"
	case "responses":
		return "Responses"
	case "gemini":
		return "Gemini"
	case "chat":
		return "Chat"
	case "images":
		return "Images"
	case "vectors":
		return "Vectors"
	}
	return channelType
}
