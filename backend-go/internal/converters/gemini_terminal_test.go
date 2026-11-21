package converters

import (
	"context"
	"encoding/json"
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

func TestGeminiWhitespaceIsNotNonStreamSuccess(t *testing.T) {
	response, err := GeminiResponseToResponses(map[string]interface{}{"candidates": []interface{}{map[string]interface{}{"finishReason": "STOP", "content": map[string]interface{}{"parts": []interface{}{map[string]interface{}{"text": "  \n\t"}}}}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != "failed" {
		t.Fatalf("whitespace status %s", response.Status)
	}
}

func TestGeminiInvalidFinishReasonTypesCannotCompleteResponses(t *testing.T) {
	for _, value := range []interface{}{42, false, map[string]interface{}{"bad": true}} {
		candidate := map[string]interface{}{"finishReason": value, "content": map[string]interface{}{"parts": []interface{}{map[string]interface{}{"text": "hello"}}}}
		raw := map[string]interface{}{"candidates": []interface{}{candidate}}
		if _, err := GeminiResponseToResponses(raw, ""); err == nil {
			t.Fatalf("nonstream accepted %#v", value)
		}
		data, _ := json.Marshal(raw)
		var state any
		events := ConvertGeminiStreamToResponses(context.Background(), "m", nil, nil, append([]byte("data: "), data...), &state)
		joined := strings.Join(events, "")
		if strings.Contains(joined, "response.completed") || !strings.Contains(joined, "INVALID_FINISH_REASON") {
			t.Fatalf("stream accepted %#v: %s", value, joined)
		}
	}
}
