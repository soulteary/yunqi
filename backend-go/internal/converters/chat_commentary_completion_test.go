package converters

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Commentary-looking text is ordinary model output, not a stream terminator.
func TestChatCommentaryDoesNotCompleteResponse(t *testing.T) {
	var state any
	request := []byte(`{"model":"deepseek-v4-pro","input":"inspect"}`)
	parts := []string{"<comm", "entary>Inspecting sources.", "</commentary>", "\nFinal answer."}
	var deltas strings.Builder
	for _, part := range parts {
		chunk, _ := json.Marshal(map[string]any{"id": "chat-commentary", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": part}, "finish_reason": nil}}})
		for _, event := range ConvertOpenAIChatToResponses(context.Background(), "deepseek-v4-pro", request, nil, append([]byte("data: "), chunk...), &state) {
			payload := strings.SplitN(event, "data: ", 2)
			var item struct {
				Type  string `json:"type"`
				Delta string `json:"delta"`
			}
			if len(payload) != 2 || json.Unmarshal([]byte(strings.TrimSpace(payload[1])), &item) != nil {
				t.Fatalf("invalid event: %s", event)
			}
			if item.Type == "response.completed" || item.Type == "response.output_text.done" {
				t.Fatalf("text prematurely completed: %s", event)
			}
			if item.Type == "response.output_text.delta" {
				deltas.WriteString(item.Delta)
			}
		}
	}
	if deltas.String() != strings.Join(parts, "") {
		t.Fatalf("text changed: %q", deltas.String())
	}
	events := ConvertOpenAIChatToResponses(context.Background(), "deepseek-v4-pro", request, nil, []byte("data: [DONE]"), &state)
	count := 0
	for _, event := range events {
		if strings.HasPrefix(event, "event: response.completed\n") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("completed events = %d", count)
	}
}
