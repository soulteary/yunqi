package converters

import (
	"encoding/json"

	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/types"
)

// Claude uses the same function proxy schemas as Chat, with input_schema on
// each tool. Preserve the original context for restoring native Codex calls.
func prepareClaudeCodexTools(sess *session.Session, req *types.ResponsesRequest) ([]map[string]interface{}, CodexToolContext) {
	raw := req.RawTools
	if len(raw) == 0 {
		for _, tool := range req.Tools {
			raw = append(raw, tool)
		}
	}
	raw = MergeCodexToolSearchOutputTools(raw, sess, req.Input)
	ctx := BuildCodexToolContextFromRaw(raw)
	if existing, ok := req.TransformerMetadata["codex_tool_context"].(CodexToolContext); ok {
		for name, spec := range existing.CustomTools {
			ctx.CustomTools[name] = spec
		}
		for name, spec := range existing.FunctionTools {
			ctx.FunctionTools[name] = spec
		}
		ctx.HasCustomTools = ctx.HasCustomTools || existing.HasCustomTools
		ctx.HasNamespaceTools = ctx.HasNamespaceTools || existing.HasNamespaceTools
		ctx.HasBuiltinFunctionTools = ctx.HasBuiltinFunctionTools || existing.HasBuiltinFunctionTools
	}
	if req.TransformerMetadata == nil {
		req.TransformerMetadata = make(map[string]interface{})
	}
	req.TransformerMetadata["codex_tool_context"] = ctx
	if _, exists := req.TransformerMetadata["codex_merged_raw_tools"]; !exists {
		req.TransformerMetadata["codex_merged_raw_tools"] = raw
	}
	return responsesToolsToClaude(responsesRawToolsToOpenAIWithContext(raw, ctx)), ctx
}

func claudeCodexHistory(items []types.ResponsesItem, ctx CodexToolContext) []types.ResponsesItem {
	result := make([]types.ResponsesItem, 0, len(items))
	for _, item := range items {
		switch item.Type {
		case "tool_search_call":
			item.Type, item.Name = "function_call", "tool_search"
			item.Arguments = toolSearchArgumentsRaw(item.Arguments)
		case "tool_search_output":
			item.Type = "function_call_output"
			if item.Output == nil {
				data, _ := json.Marshal(map[string]interface{}{"tools": item.Tools})
				item.Output = string(data)
			}
		case "custom_tool_call":
			item.Type = "function_call"
			item.Name, item.Arguments = BuildCustomToolCallHistoryArguments(ctx, item.Name, customToolInputFromItem(item))
		case "custom_tool_call_output":
			item.Type = "function_call_output"
		case "function_call":
			if name, ok := ctx.UpstreamNameForNamespaceCall(item.Namespace, item.Name); ok {
				item.Name, item.Namespace = name, ""
			}
		}
		result = append(result, item)
	}
	return result
}

func claudeToolChoice(choice interface{}, ctx CodexToolContext) interface{} {
	choice = ConvertToolChoiceForCodex(choice, ctx)
	if name, ok := choice.(string); ok {
		switch name {
		case "required":
			return map[string]interface{}{"type": "any"}
		case "auto", "none":
			return map[string]interface{}{"type": name}
		}
	}
	if value, ok := choice.(map[string]interface{}); ok {
		if value["type"] == "tool_search" && ctx.IsToolSearchProxy("tool_search") {
			return map[string]interface{}{"type": "tool", "name": "tool_search"}
		}
		if value["type"] == "function" {
			function, _ := value["function"].(map[string]interface{})
			name, _ := function["name"].(string)
			if name == "" {
				name, _ = value["name"].(string)
			}
			if name != "" {
				return map[string]interface{}{"type": "tool", "name": name}
			}
		}
	}
	return nil
}
