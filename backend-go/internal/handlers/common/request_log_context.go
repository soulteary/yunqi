package common

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/types"
)

const (
	requestLogContextKey = "requestLogContext"
	consoleJSONTextLimit = 1000
)

type httpRequestLogContextKey struct{}

type RequestLogContext struct {
	SessionID string
	Round     int
	AgentCtx  *types.AgentContext
	AgentRole string
}

func SetRequestLogContext(c fiber.Ctx, sessionID string, round int) {
	SetRequestLogContextWithAgent(c, sessionID, round, nil)
}

// SetRequestLogContextWithAgent 设置请求日志上下文，附带代理上下文信息。
func SetRequestLogContextWithAgent(c fiber.Ctx, sessionID string, round int, agentCtx *types.AgentContext) {
	if c == nil {
		return
	}
	agentRole := ""
	if agentCtx != nil {
		agentRole = agentCtx.AgentRole
	}
	httpbridge.SetValue(c, requestLogContextKey, RequestLogContext{
		SessionID: strings.TrimSpace(sessionID),
		Round:     round,
		AgentCtx:  agentCtx,
		AgentRole: agentRole,
	})
}

// AgentContextFromFiber 从 fiber.Ctx 读取代理上下文，供日志记录使用。
// 返回带 SessionID 填充的副本（nil 安全）。
// AgentContextFromFiber 从 fiber.Ctx 读取代理上下文，供日志记录使用。
// 返回 AgentContext 的副本（nil 安全）。SessionID 由 CreatePendingLog 单独从日志上下文填充。
func AgentContextFromFiber(c fiber.Ctx) *types.AgentContext {
	ctx, ok := requestLogContextFromFiber(c)
	if !ok || ctx.AgentCtx == nil {
		return nil
	}
	snapshot := *ctx.AgentCtx
	return &snapshot
}

// RequestConversationContextFromFiber 获取请求对应的会话上下文（nil 安全）。
func RequestConversationContextFromFiber(c fiber.Ctx) (sessionID string, round int, agentRole string, ok bool) {
	ctx, ok := requestLogContextFromFiber(c)
	if !ok {
		return "", 0, "", false
	}
	return ctx.SessionID, ctx.Round, ctx.AgentRole, true
}

// sessionIDFromFiber 从 fiber.Ctx 读取会话标识（用于 ChannelLog.SessionID 关联）。
// SessionIDFromFiber 从 fiber.Ctx 读取会话标识（用于 ChannelLog.SessionID 关联）。
func SessionIDFromFiber(c fiber.Ctx) string {
	ctx, ok := requestLogContextFromFiber(c)
	if !ok {
		return ""
	}
	return ctx.SessionID
}

func RequestLogTag(c fiber.Ctx) string {
	ctx, ok := requestLogContextFromFiber(c)
	if !ok {
		return ""
	}
	return requestLogTag(ctx)
}

func WithRequestLogContext(req *http.Request, c fiber.Ctx) *http.Request {
	if req == nil {
		return nil
	}
	ctx, ok := requestLogContextFromFiber(c)
	if !ok {
		return req
	}
	return req.WithContext(context.WithValue(req.Context(), httpRequestLogContextKey{}, ctx))
}

func requestLogTagFromRequest(req *http.Request) string {
	if req == nil {
		return ""
	}
	ctx, ok := req.Context().Value(httpRequestLogContextKey{}).(RequestLogContext)
	if !ok {
		return ""
	}
	return requestLogTag(ctx)
}

func requestLogContextFromFiber(c fiber.Ctx) (RequestLogContext, bool) {
	if c == nil {
		return RequestLogContext{}, false
	}
	value, ok := httpbridge.Value(c, requestLogContextKey)
	if !ok {
		return RequestLogContext{}, false
	}
	ctx, ok := value.(RequestLogContext)
	if !ok {
		return RequestLogContext{}, false
	}
	return ctx, true
}

func requestLogTag(ctx RequestLogContext) string {
	parts := make([]string, 0, 2)
	if ctx.SessionID != "" {
		parts = append(parts, "session="+scheduler.MaskUserIDForLog(ctx.SessionID))
	}
	if ctx.Round > 0 {
		parts = append(parts, fmt.Sprintf("round=%d", ctx.Round))
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func RequestLogf(c fiber.Ctx, format string, args ...interface{}) {
	tag := RequestLogTag(c)
	logWithTag(tag, format, args...)
}

func LogWithTag(tag string, format string, args ...interface{}) {
	logWithTag(tag, format, args...)
}

func requestLogToConsole(c fiber.Ctx, format string, args ...interface{}) {
	tag := RequestLogTag(c)
	if tag == "" {
		logToConsole(format, args...)
		return
	}
	logToConsole(taggedFormat(format, tag), args...)
}

func requestLogToFile(c fiber.Ctx, format string, args ...interface{}) {
	tag := RequestLogTag(c)
	if tag == "" {
		logToFile(format, args...)
		return
	}
	logToFile(taggedFormat(format, tag), args...)
}

func requestLogToConsoleFromRequest(req *http.Request, format string, args ...interface{}) {
	tag := requestLogTagFromRequest(req)
	if tag == "" {
		logToConsole(format, args...)
		return
	}
	logToConsole(taggedFormat(format, tag), args...)
}

func requestLogToFileFromRequest(req *http.Request, format string, args ...interface{}) {
	tag := requestLogTagFromRequest(req)
	if tag == "" {
		logToFile(format, args...)
		return
	}
	logToFile(taggedFormat(format, tag), args...)
}

func logWithTag(tag string, format string, args ...interface{}) {
	if tag == "" {
		log.Printf(format, args...)
		return
	}
	log.Printf(taggedFormat(format, tag), args...)
}

func taggedFormat(format string, tag string) string {
	if tag == "" {
		return format
	}
	if strings.HasPrefix(format, "[") {
		if idx := strings.Index(format, "]"); idx >= 0 {
			return format[:idx+1] + " " + tag + format[idx+1:]
		}
	}
	return tag + " " + format
}
