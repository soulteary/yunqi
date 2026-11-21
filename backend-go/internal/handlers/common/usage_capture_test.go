package common

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/types"
	"io"
	"strings"
	"testing"
)

func TestUsageCapturePreservesRawAcrossProtocols(t *testing.T) {
	for _, tc := range []struct {
		name, body, protocol string
		stream               bool
	}{
		{"chat", `{"text":"secret response","usage":{"prompt_tokens":12,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":3}}}`, "openai", false},
		{"messages", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":12}}}\n\ndata: {\"usage\":{\"output_tokens\":4,\"cache_read_input_tokens\":3}}\n\n", "claude", true},
		{"responses", "data: {\"response\":{\"usage\":{\"input_tokens\":12,\"output_tokens\":4,\"input_tokens_details\":{\"cached_tokens\":3}}}}\n\n", "responses", true},
		{"gemini", `{"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":4,"cachedContentTokenCount":3}}`, "gemini", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &usageCapture{ReadCloser: io.NopCloser(strings.NewReader(tc.body)), stream: tc.stream}
			// Read in tiny chunks to exercise fragmented SSE frames and JSON objects.
			var got strings.Builder
			buf := make([]byte, 3)
			for {
				n, err := r.Read(buf)
				got.Write(buf[:n])
				if err != nil {
					break
				}
			}
			if got.String() != tc.body {
				t.Fatal("observer modified body")
			}
			u := &types.Usage{InputTokens: 9, OutputTokens: 4, CacheReadInputTokens: 3}
			r.annotate(u, tc.protocol)
			p := u.AccountingProvenance()
			if p.UsageSource != "provider" || len(p.RawUsage) == 0 || len(p.EstimatedFields) > 0 {
				t.Fatalf("%+v", p)
			}
			if strings.Contains(string(p.RawUsage), "secret") {
				t.Fatal("response content leaked")
			}
			wire, _ := json.Marshal(u)
			if strings.Contains(string(wire), "provenance") || strings.Contains(string(wire), "usage_source") {
				t.Fatal("internal accounting leaked on wire")
			}
		})
	}
}

func TestUsageCaptureMarksMissingAndZeroFieldsEstimated(t *testing.T) {
	r := &usageCapture{ReadCloser: io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":5,"output_tokens":0}}`))}
	_, _ = io.ReadAll(r)
	u := &types.Usage{InputTokens: 5, OutputTokens: 8}
	r.annotate(u, "messages")
	if u.Provenance.UsageSource != "mixed" || len(u.Provenance.EstimatedFields) != 1 || u.Provenance.EstimatedFields[0] != "output_tokens" {
		t.Fatalf("%+v", u.Provenance)
	}
}

func TestUsageCaptureMissingUsageIsExplicitEstimate(t *testing.T) {
	r := &usageCapture{ReadCloser: io.NopCloser(strings.NewReader(`{"content":[]}`))}
	_, _ = io.ReadAll(r)
	u := &types.Usage{InputTokens: 3, OutputTokens: 8}
	r.annotate(u, "messages")
	if u.Provenance.UsageSource != "estimated" || len(u.Provenance.EstimatedFields) != 2 || len(u.Provenance.RawUsage) > 0 {
		t.Fatalf("%+v", u.Provenance)
	}
}

func TestUsageCaptureLargeJSONIsBoundedUnknown(t *testing.T) {
	r := &usageCapture{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("x", 3<<20)))}
	_, _ = io.ReadAll(r)
	if len(r.pending) > 2<<20 {
		t.Fatal("unbounded observation buffer")
	}
	u := &types.Usage{InputTokens: 3}
	r.annotate(u, "messages")
	if u.Provenance.UsageSource != "unknown" {
		t.Fatalf("%+v", u.Provenance)
	}
}

func TestUsageCaptureAuthoritativeTerminalZeroCorrectsEstimate(t *testing.T) {
	body := "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n"
	r := &usageCapture{ReadCloser: io.NopCloser(strings.NewReader(body)), stream: true}
	_, _ = io.ReadAll(r)
	u := &types.Usage{InputTokens: 5, OutputTokens: 32}
	r.annotate(u, "responses")
	if u.OutputTokens != 0 || u.Provenance.UsageSource != "provider" || len(u.Provenance.EstimatedFields) != 0 {
		t.Fatalf("%+v %+v", u, u.Provenance)
	}
}
