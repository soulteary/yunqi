package converters

import (
	"reflect"
	"testing"

	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/types"
)

func TestClaudeToolSearchCompatibilityControls(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		req := &types.ResponsesRequest{Model: "test-model", Input: "search", Tools: []map[string]interface{}{
			{"type": "function", "name": "ordinary", "parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}}}},
			{"type": "tool_search", "execution": "client"},
		}, ToolChoice: map[string]interface{}{"type": "tool_search"}, TransformerMetadata: map[string]interface{}{"codex_tool_compat_enabled": enabled}}
		converted, err := (&ClaudeConverter{}).ToProviderRequest(&session.Session{}, req)
		if err != nil {
			t.Fatal(err)
		}
		body := converted.(map[string]interface{})
		tools := body["tools"].([]map[string]interface{})
		want := 1
		if enabled {
			want = 2
		}
		if len(tools) != want || tools[0]["name"] != "ordinary" {
			t.Fatalf("enabled=%t tools=%v", enabled, tools)
		}
		if !reflect.DeepEqual(tools[0]["input_schema"].(map[string]interface{})["properties"], req.Tools[0]["parameters"].(map[string]interface{})["properties"]) {
			t.Fatal("ordinary tool schema changed")
		}
		if enabled {
			if !reflect.DeepEqual(body["tool_choice"], map[string]interface{}{"type": "tool", "name": "tool_search"}) {
				t.Fatalf("choice=%v", body["tool_choice"])
			}
		} else if _, ok := body["tool_choice"]; ok {
			t.Fatal("disabled compatibility changed legacy choice behavior")
		}
	}
}
