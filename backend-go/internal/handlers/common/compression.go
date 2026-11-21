package common

import (
	"log"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/compression"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
)

const (
	// CompressionHeader 请求头控制压缩开关。
	CompressionHeader = "X-Ccx-Compression"
)

// CompressionContext 存储请求级压缩上下文（结果统计）。
type CompressionContext struct {
	OriginalTokens   int
	CompressedTokens int
	SavingsPercent   float64
	Compressed       bool
	Technique        string
	FallbackReason   string
}

// compressionContextKey 是 Fiber context 中压缩上下文的 key。
const compressionContextKey = "compressionContext"

// ApplyRequestCompression 在转发主链中应用请求侧 tool_result 压缩。
// 复用 guardrails 落定的钩子姿势：在 TryUpstreamWithAllKeys 入口附近调用。
//
// 只对 messages 类入口生效；images/vectors 入口不适用。
// fail-open：任何异常（panic / 解析失败）均返回原文，不阻断请求。
func ApplyRequestCompression(
	c fiber.Ctx,
	bodyBytes []byte,
	kind scheduler.ChannelKind,
	scenarioKey string,
	globalEnabled bool,
	channelEnabled bool,
) []byte {
	if len(bodyBytes) == 0 {
		return bodyBytes
	}

	// 仅 messages 类入口压缩
	switch kind {
	case scheduler.ChannelKindMessages,
		scheduler.ChannelKindChat,
		scheduler.ChannelKindResponses,
		scheduler.ChannelKindGemini:
		// 适用
	default:
		return bodyBytes
	}

	// 解析压缩计划
	headerCompression := httpbridge.GetHeader(c, CompressionHeader)
	plan := compression.ResolvePlan(headerCompression, scenarioKey, globalEnabled, channelEnabled)
	if !plan.Enabled {
		return bodyBytes
	}

	// 执行压缩
	result, err := compression.CompressRequestBody(bodyBytes, plan)
	if err != nil {
		log.Printf("[Compression-Apply] 压缩失败，fail-open: %v", err)
		return bodyBytes
	}

	// 写入 Fiber context 供日志/遥测使用
	if result.Compressed {
		ctx := &CompressionContext{
			OriginalTokens:   result.OriginalTokens,
			CompressedTokens: result.CompressedTokens,
			SavingsPercent:   result.SavingsPercent,
			Compressed:       true,
			Technique:        result.Technique,
		}
		httpbridge.SetValue(c, compressionContextKey, ctx)
	} else if result.FallbackReason != "" {
		ctx := &CompressionContext{
			Compressed:     false,
			FallbackReason: result.FallbackReason,
			OriginalTokens: result.OriginalTokens,
		}
		httpbridge.SetValue(c, compressionContextKey, ctx)
	}

	return result.Body
}

// GetCompressionContext 从 Fiber context 读取压缩统计（用于日志、metrics、成本报表）。
func GetCompressionContext(c fiber.Ctx) *CompressionContext {
	if c == nil {
		return nil
	}
	if val, ok := httpbridge.Value(c, compressionContextKey); ok {
		if ctx, ok := val.(*CompressionContext); ok {
			return ctx
		}
	}
	return nil
}
