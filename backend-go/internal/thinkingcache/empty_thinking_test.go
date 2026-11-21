package thinkingcache

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/types"
)

func emptyThinkingToolContent(id string) []interface{} {
	return []interface{}{
		map[string]interface{}{"type": "thinking", "thinking": ""},
		map[string]interface{}{"type": "tool_use", "id": id, "name": "Bash", "input": map[string]interface{}{"command": "pwd"}},
	}
}

func TestEmptyThinkingPresencePersistsWithoutSessionFallback(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "sqlite"}[persistent], func(t *testing.T) {
			ResetForTest()
			t.Cleanup(ResetForTest)
			path := filepath.Join(t.TempDir(), "thinking.db")
			if persistent {
				if err := Configure(Config{DBPath: path, TTL: time.Hour}); err != nil {
					t.Fatal(err)
				}
			}
			content := emptyThinkingToolContent("tool_a")
			if !StoreClaudeThinkingForContent("session", content, "old nonempty reasoning") {
				t.Fatal("store old reasoning")
			}
			if !StoreClaudeResponseThinking("session", observedThinkingContent(t, content)) {
				t.Fatal("explicit empty thinking was not stored")
			}
			if persistent {
				ResetForTest()
				if err := Configure(Config{DBPath: path, TTL: time.Hour}); err != nil {
					t.Fatal(err)
				}
			}
			got, ok := LookupClaudeThinkingForContent("session", content[1:])
			if !ok || got != "" {
				t.Fatalf("lookup=(%q,%t), want explicit empty hit", got, ok)
			}
			if got, ok := LookupLatestClaudeThinkingForSession("session"); ok {
				t.Fatalf("empty thinking eligible for session fallback: %q", got)
			}
			if _, ok := LookupClaudeThinkingForContent("other-session", content[1:]); ok {
				t.Fatal("thinking leaked to other session")
			}
			target := &config.UpstreamConfig{ServiceType: "claude", BaseURL: "https://api.deepseek.com"}
			for _, id := range []string{"tool_a", "tool_b"} {
				requestContent := emptyThinkingToolContent(id)[1:]
				data, _ := json.Marshal(map[string]interface{}{"model": "deepseek-v4-pro", "thinking": map[string]interface{}{"type": "enabled"}, "messages": []interface{}{map[string]interface{}{"role": "assistant", "content": requestContent}}})
				result, injected := InjectCachedClaudeThinking(data, "session", target)
				want := 0
				if id == "tool_a" {
					want = 1
				}
				if injected != want {
					t.Fatalf("tool=%s injected=%d want %d body=%s", id, injected, want, result)
				}
			}
			// An exact empty block already echoed by the client retains its position and is not duplicated.
			data, _ := json.Marshal(map[string]interface{}{"model": "deepseek-v4-pro", "thinking": map[string]interface{}{"type": "enabled"}, "messages": []interface{}{map[string]interface{}{"role": "assistant", "content": content}}})
			result, injected := InjectCachedClaudeThinking(data, "session", target)
			if injected != 0 || string(result) != string(data) {
				t.Fatalf("existing empty thinking changed: %s", result)
			}
			absent := emptyThinkingToolContent("tool_absent")[1:]
			if StoreClaudeResponseThinking("session", observedThinkingContent(t, absent)) {
				t.Fatal("missing thinking created cache entry")
			}
			if StoreClaudeThinkingForContent("session", absent, "") {
				t.Fatal("legacy string-only API must not infer empty presence")
			}
			if StoreClaudeResponseThinking("session", []types.ClaudeContent{{Type: "redacted_thinking"}}) {
				t.Fatal("redacted data without thinking text must not create empty thinking")
			}
		})
	}
}

func TestClaudeStreamCollectorDistinguishesAbsentAndEmptyThinking(t *testing.T) {
	for _, seen := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "empty"}[seen], func(t *testing.T) {
			ResetForTest()
			t.Cleanup(ResetForTest)
			collector := NewClaudeStreamCollector()
			if seen {
				collector.ProcessEvent(`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
			}
			collector.ProcessEvent(`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_a","name":"Bash","input":{"command":"pwd"}}}`)
			if got := collector.Store("session"); got != seen {
				t.Fatalf("Store=%t want %t", got, seen)
			}
			value, ok := LookupClaudeThinkingForContent("session", emptyThinkingToolContent("tool_a")[1:])
			if ok != seen || value != "" {
				t.Fatalf("lookup=(%q,%t) want empty hit=%t", value, ok, seen)
			}
		})
	}
}

func observedThinkingContent(t *testing.T, content interface{}) []types.ClaudeContent {
	t.Helper()
	raw, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	var blocks []types.ClaudeContent
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatal(err)
	}
	return blocks
}
