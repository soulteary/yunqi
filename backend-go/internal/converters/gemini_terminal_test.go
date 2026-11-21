package converters

import (
	"context"
	"strings"
	"testing"
)

func TestGeminiThoughtOnlyDoesNotCompleteResponses(t *testing.T) {
	var state any
	events := ConvertGeminiStreamToResponses(context.Background(), "m", nil, nil, []byte(`data: {"candidates":[{"content":{"parts":[{"text":"thinking","thought":true}]},"finishReason":"STOP"}]}`), &state)
	joined := strings.Join(events, "")
	if strings.Contains(joined, "response.completed") || !strings.Contains(joined, "response.failed") || !strings.Contains(joined, "NO_DELIVERABLE_OUTPUT") {
		t.Fatalf("thought-only terminal %s", joined)
	}
	response, err := GeminiResponseToResponses(map[string]interface{}{"candidates": []interface{}{map[string]interface{}{"finishReason": "STOP", "content": map[string]interface{}{"parts": []interface{}{map[string]interface{}{"text": "thinking", "thought": true}}}}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "failed" {
		t.Fatalf("thought-only status %s", response.Status)
	}
}
