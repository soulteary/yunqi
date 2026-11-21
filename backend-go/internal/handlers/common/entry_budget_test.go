package common

import (
	"io"
	"strings"
	"testing"
)

func TestBudgetProviderUsageRequiresCompleteAndValidCounters(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, body string
		stream, known        bool
		input, output, cache int
	}{
		{"zero", "chat", `{"usage":{"prompt_tokens":0,"completion_tokens":0}}`, false, true, 0, 0, 0},
		{"cached-inclusive", "responses", `{"usage":{"input_tokens":10,"output_tokens":4,"input_tokens_details":{"cached_tokens":3}}}`, false, true, 7, 4, 3},
		{"gemini-thoughts", "gemini", `{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"thoughtsTokenCount":5,"totalTokenCount":17,"cachedContentTokenCount":3}}`, false, true, 7, 7, 3},
		{"missing-output", "chat", `{"usage":{"prompt_tokens":10}}`, false, false, 0, 0, 0},
		{"bad-string", "chat", `{"usage":{"prompt_tokens":"10","completion_tokens":0}}`, false, false, 0, 0, 0},
		{"negative", "chat", `{"usage":{"prompt_tokens":10,"completion_tokens":-1}}`, false, false, 0, 0, 0},
		{"fractional", "chat", `{"usage":{"prompt_tokens":10,"completion_tokens":1.5}}`, false, false, 0, 0, 0},
		{"invalid-cache-type", "chat", `{"usage":{"prompt_tokens":2,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":"2"}}}`, false, false, 0, 0, 0},
		{"invalid-cache", "chat", `{"usage":{"prompt_tokens":2,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":3}}}`, false, false, 0, 0, 0},
		{"truncated-stream", "responses", "data: {\"type\":\"response.in_progress\",\"response\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n", true, false, 0, 0, 0},
		{"terminal-zero", "responses", "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n", true, true, 0, 0, 0},
		{"chat-done", "chat", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n", true, true, 2, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capture := &usageCapture{ReadCloser: io.NopCloser(strings.NewReader(tc.body)), stream: tc.stream}
			if _, err := io.Copy(io.Discard, capture); err != nil {
				t.Fatal(err)
			}
			usage, known := capture.billingUsage(tc.protocol)
			if known != tc.known {
				t.Fatalf("known=%v usage=%+v raw=%s", known, usage, tc.body)
			}
			if known && (usage.InputTokens != tc.input || usage.OutputTokens != tc.output || usage.CacheReadInputTokens != tc.cache) {
				t.Fatalf("wrong canonical accounting %+v", usage)
			}
		})
	}
}
