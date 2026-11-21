package converters

import (
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/types"
)

// ============== Claude Messages API 转换器 ==============

// ClaudeConverter 实现 Responses → Claude Messages API 转换
type ClaudeConverter struct{}

// ToProviderRequest 将 Responses 请求转换为 Claude Messages 格式
func (c *ClaudeConverter) ToProviderRequest(sess *session.Session, req *types.ResponsesRequest) (interface{}, error) {
	var codexTools []map[string]interface{}
	var codexCtx CodexToolContext
	codexEnabled, _ := req.TransformerMetadata["codex_tool_compat_enabled"].(bool)
	input := req.Input
	if codexEnabled {
		codexTools, codexCtx = prepareClaudeCodexTools(sess, req)
		items, err := parseResponsesInput(input)
		if err != nil {
			return nil, err
		}
		input = claudeCodexHistory(items, codexCtx)
		sess = &session.Session{Messages: claudeCodexHistory(sess.Messages, codexCtx)}
	}
	// 转换 messages 和 system
	messages, system, err := ResponsesToClaudeMessages(sess, input, req.Instructions)
	if err != nil {
		return nil, err
	}

	// 构建 Claude 请求
	claudeReq := map[string]interface{}{
		"model":    req.Model,
		"messages": messages,
		"stream":   req.Stream,
	}

	// Claude 使用独立的 system 参数（不在 messages 中）
	if system != "" {
		claudeReq["system"] = system
	}

	// 复制其他参数
	if req.MaxTokens > 0 {
		claudeReq["max_tokens"] = req.MaxTokens
	} else {
		claudeReq["max_tokens"] = 4096
	}
	if req.Temperature > 0 {
		claudeReq["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		claudeReq["top_p"] = req.TopP
	}
	if req.Stop != nil {
		claudeReq["stop_sequences"] = req.Stop // Claude 使用 stop_sequences
	}
	if req.User != "" {
		claudeReq["metadata"] = map[string]interface{}{"user_id": req.User}
	}
	if codexEnabled {
		if len(codexTools) > 0 {
			claudeReq["tools"] = codexTools
		}
	} else if len(req.Tools) > 0 {
		if tools := responsesToolsToClaude(req.Tools); len(tools) > 0 {
			claudeReq["tools"] = tools
		}
	}
	if _, hasTools := claudeReq["tools"]; codexEnabled && hasTools && req.ToolChoice != nil {
		if choice := claudeToolChoice(req.ToolChoice, codexCtx); choice != nil {
			claudeReq["tool_choice"] = choice
		}
	}

	return claudeReq, nil
}

// FromProviderResponse 将 Claude 响应转换为 Responses 格式
func (c *ClaudeConverter) FromProviderResponse(resp map[string]interface{}, sessionID string) (*types.ResponsesResponse, error) {
	return ClaudeResponseToResponses(resp, sessionID)
}

// GetProviderName 获取上游服务名称
func (c *ClaudeConverter) GetProviderName() string {
	return "Claude Messages API"
}
