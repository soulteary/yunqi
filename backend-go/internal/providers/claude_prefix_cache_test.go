package providers

import (
	"context"
	"encoding/json"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"testing"
)

func TestResponsesClaudePrefixCacheWire(t *testing.T) {
	multi := `[{"type":"message","role":"user","content":"one"},{"type":"message","role":"assistant","content":"answer"},{"type":"message","role":"user","content":"two"}]`
	for _, tc := range []struct {
		name, input string
		enabled     bool
		want        int
	}{
		{"default off", multi, false, 0},
		{"single turn", `"hi"`, true, 0},
		{"multi turn", multi, true, 1},
		{"explicit wins", `[{"type":"message","role":"user","content":[{"type":"input_text","text":"one","cache_control":{"type":"ephemeral","ttl":"1h"}}]},{"type":"message","role":"assistant","content":"answer"},{"type":"message","role":"user","content":"two"}]`, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"model":"m","input":` + tc.input + `}`)
			c := newFiberContext(http.MethodPost, "/v1/responses", raw, context.Background())
			req, _, err := (&ResponsesProvider{}).ConvertToProviderRequest(c, &config.UpstreamConfig{BaseURL: "https://example.invalid", ServiceType: "claude", ResponsesClaudePrefixCache: tc.enabled}, "test")
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			var parsed interface{}
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Fatal(err)
			}
			if got := countPrefixBreakpoints(parsed); got != tc.want {
				t.Fatalf("breakpoints=%d want=%d: %s", got, tc.want, body)
			}
			if tc.name == "explicit wins" && gjson.GetBytes(body, "messages.0.content.0.cache_control.ttl").String() != "1h" {
				t.Fatalf("explicit TTL lost %s", body)
			}
		})
	}
}

func countPrefixBreakpoints(value interface{}) int {
	total := 0
	switch node := value.(type) {
	case map[string]interface{}:
		for key, child := range node {
			if key == "cache_control" {
				total++
			}
			total += countPrefixBreakpoints(child)
		}
	case []interface{}:
		for _, child := range node {
			total += countPrefixBreakpoints(child)
		}
	}
	return total
}

func TestClaudePrefixCacheNeverExceedsFourExplicitBreakpoints(t *testing.T) {
	request := map[string]interface{}{"messages": []interface{}{}, "tools": []interface{}{}}
	for i := 0; i < 4; i++ {
		request["tools"] = append(request["tools"].([]interface{}), map[string]interface{}{"cache_control": map[string]interface{}{"type": "ephemeral"}})
	}
	applyClaudePrefixCache(request)
	if got := countPrefixBreakpoints(request); got != 4 {
		t.Fatalf("breakpoints=%d", got)
	}
}
