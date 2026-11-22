package common

import (
	"context"
	"encoding/json"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/entrybudget"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBudgetProviderUsageRequiresCompleteAndValidCounters(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, body string
		stream, known        bool
		input, output, cache int
	}{
		{"zero", "chat", `{"usage":{"prompt_tokens":0,"completion_tokens":0}}`, false, true, 0, 0, 0},
		{"cached-inclusive", "responses", `{"usage":{"input_tokens":10,"output_tokens":4,"input_tokens_details":{"cached_tokens":3}}}`, false, true, 7, 4, 3},
		{"gemini-thoughts", "gemini", `{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"thoughtsTokenCount":5,"totalTokenCount":17,"cachedContentTokenCount":3}}`, false, true, 7, 2, 3},
		{"gemini-conflicting-total", "gemini", `{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":10000,"totalTokenCount":10}}`, false, false, 0, 0, 0},
		{"gemini-tool-input", "gemini", `{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"toolUsePromptTokenCount":3,"totalTokenCount":15}}`, false, false, 0, 0, 0},
		{"gemini-invalid-total", "gemini", `{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":"12"}}`, false, false, 0, 0, 0},
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

func TestBudgetProtocolUsesFinalEndpointNotMisleadingBasePrefix(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"/chat/completions-proxy/v1/messages", "messages"},
		{"/responses-proxy/v1/chat/completions", "chat"},
		{"/models/fake:generateContent/v1/responses", "responses"},
		{"/chat/completions-proxy/v1beta/models/gemini:generateContent", "gemini"},
		{"/images/proxy/v1/embeddings", "vectors"},
	} {
		req, _ := http.NewRequest("POST", "https://example.invalid"+tc.path, nil)
		if got := budgetProtocol(req, &config.UpstreamConfig{ServiceType: "openai"}); got != tc.want {
			t.Fatalf("%s classified %s want %s", tc.path, got, tc.want)
		}
	}
	capture := &usageCapture{ReadCloser: io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":3}}`))}
	_, _ = io.Copy(io.Discard, capture)
	req, _ := http.NewRequest("POST", "https://example.invalid/chat/completions-proxy/v1/messages", nil)
	usage, known := capture.billingUsage(budgetProtocol(req, &config.UpstreamConfig{}))
	if !known || usage.InputTokens != 10 || usage.CacheReadInputTokens != 3 {
		t.Fatalf("misleading prefix changed Claude exclusive input accounting: %+v %v", usage, known)
	}
}
func TestBudgetHostedToolsFailClosedButClientFunctionsRemainPriced(t *testing.T) {
	for _, tc := range []struct {
		tool   string
		reject bool
	}{
		{`{"type":"web_search_20250305"}`, true}, {`{"type":"web_search_preview"}`, true}, {`{"type":"file_search"}`, true}, {`{"googleSearch":{}}`, true}, {`{"google_search_retrieval":{}}`, true},
		{`{"type":"function","name":"web_search","parameters":{"type":"object","properties":{"googleSearch":{"type":"string"}}}}`, false},
		{`{"type":"custom","name":"file_search"}`, false},
		{`{"functionDeclarations":[{"name":"googleSearch","parameters":{"type":"object"}}]}`, false},
	} {
		var value interface{}
		if err := json.Unmarshal([]byte(`{"tools":[`+tc.tool+`]}`), &value); err != nil {
			t.Fatal(err)
		}
		if got := budgetHasUnpricedServerTools(value); got != tc.reject {
			t.Fatalf("%s reject=%v", tc.tool, got)
		}
	}
}

func TestBudgetZeroUsageWithoutRealSourceTerminalKeepsConservativeCharge(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, body string
		want                 int64
	}{
		{"claude-stop-reason-only", "messages", "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}\n\n", 50},
		{"chat-usage-only", "chat", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0}}\n\n", 50},
		{"gemini-placeholder", "gemini", "data: {\"candidates\":[{\"finishReason\":\"FINISH_REASON_UNSPECIFIED\"}],\"usageMetadata\":{\"promptTokenCount\":0,\"candidatesTokenCount\":0,\"totalTokenCount\":0}}\n\n", 50},
		{"responses-bare-done", "responses", "data: {\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}\n\ndata: [DONE]\n\n", 50},
		{"responses-real-terminal", "responses", "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n", 0},
		{"chat-real-done", "chat", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0}}\n\ndata: [DONE]\n\n", 0},
		{"chat-real-finish", "chat", "data: {\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0}}\n\ndata: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n", 0},
		{"gemini-real-terminal", "gemini", "data: {\"usageMetadata\":{\"promptTokenCount\":0,\"candidatesTokenCount\":0,\"totalTokenCount\":0}}\n\ndata: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n", 0},
		{"claude-real-stop", "messages", "data: {\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}\n\ndata: {\"type\":\"message_stop\"}\n\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := entrybudget.NewManager(filepath.Join(t.TempDir(), "budget.db"))
			defer manager.Close()
			policy := entrybudget.Policy{PrincipalID: "terminal", Period: "lifetime", LimitNanoUSD: 100, RequestCapNanoUSD: 100}
			session, err := manager.Start(context.Background(), policy, "logical")
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := session.Begin(context.Background(), entrybudget.AttemptQuote{Model: "m", Amount: 50, PricingVersion: "v", Pricing: json.RawMessage(`{"outputPrice":1}`)})
			if err != nil {
				t.Fatal(err)
			}
			price := 1.0
			capture := &usageCapture{ReadCloser: io.NopCloser(strings.NewReader(tc.body)), stream: true}
			body := &budgetResponseBody{capture: capture, attempt: attempt, pricing: &config.ModelPricing{InputCacheMissPrice: &price, OutputPrice: &price}, protocol: tc.protocol, status: 200}
			if _, err := io.Copy(io.Discard, body); err != nil {
				t.Fatal(err)
			}
			if err := body.Close(); err != nil {
				t.Fatal(err)
			}
			session.ParentDone()
			store, _ := manager.Store()
			state, err := store.Snapshot(context.Background(), policy, time.Now())
			if err != nil || state.Spent != tc.want || state.Reserved != 0 {
				t.Fatalf("source terminal billing %+v %v", state, err)
			}
		})
	}
}

func TestBudgetUsesFrozenTypedPricesForReservationAndProviderSettlement(t *testing.T) {
	pointer := func(n float64) *float64 { return &n }
	for _, tc := range []struct {
		name, protocol, path, raw                 string
		pricing                                   *config.ModelPricing
		want, minimumInputRate, minimumOutputRate int64
	}{
		{"claude-cache-ttl", "messages", "/v1/messages", `{"usage":{"input_tokens":10,"output_tokens":2,"cache_creation_input_tokens":5,"cache_creation":{"ephemeral_5m_input_tokens":2,"ephemeral_1h_input_tokens":3}}}`, &config.ModelPricing{InputCacheMissPrice: pointer(1), OutputPrice: pointer(2), CacheCreation5mPrice: pointer(3), CacheCreation1hPrice: pointer(5)}, 35000, 5, 2},
		{"openai-reasoning-subset", "chat", "/v1/chat/completions", `{"usage":{"prompt_tokens":10,"completion_tokens":10,"completion_tokens_details":{"reasoning_tokens":4}}}`, &config.ModelPricing{InputCacheMissPrice: pointer(1), OutputPrice: pointer(2), ReasoningOutputPrice: pointer(5)}, 42000, 1, 7},
		{"gemini-thoughts-once", "gemini", "/v1beta/models/m:generateContent", `{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"thoughtsTokenCount":5,"totalTokenCount":17}}`, &config.ModelPricing{InputCacheMissPrice: pointer(1), OutputPrice: pointer(2), ReasoningOutputPrice: pointer(5)}, 39000, 1, 7},
		{"reachable-expensive-lower-tier", "chat", "/v1/chat/completions", `{"usage":{"prompt_tokens":2,"completion_tokens":1}}`, &config.ModelPricing{InputCacheMissPrice: pointer(1), OutputPrice: pointer(1), Tiers: []config.ModelPricingTier{{InputTokensUpTo: 100, InputCacheMissPrice: pointer(10)}}}, 21000, 10, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := entrybudget.NewManager(filepath.Join(t.TempDir(), "typed.db"))
			defer manager.Close()
			policy := entrybudget.Policy{PrincipalID: "typed", Period: "lifetime", LimitNanoUSD: 1000000000, RequestCapNanoUSD: 1000000000}
			session, err := manager.Start(context.Background(), policy, "logical")
			if err != nil {
				t.Fatal(err)
			}
			requestBody := `{"model":"m","max_tokens":100,"messages":[{"role":"user","content":"hello"}]}`
			req, err := http.NewRequestWithContext(entrybudget.WithSession(context.Background(), session), "POST", "https://example.invalid"+tc.path, strings.NewReader(requestBody))
			if err != nil {
				t.Fatal(err)
			}
			upstream := &config.UpstreamConfig{ChannelUID: "ch-frozen", ModelCapabilities: map[string]config.UpstreamModelCapability{"m": {Pricing: tc.pricing}}}
			attempt, snapshot, protocol, err := beginEntryBudget(req, upstream, &config.EnvConfig{})
			if err != nil {
				t.Fatal(err)
			}
			if protocol != tc.protocol || snapshot == tc.pricing || snapshot.VersionID != config.PricingVersionID(snapshot) || attempt.Quote.PricingVersion != snapshot.VersionID {
				t.Fatalf("missing frozen typed snapshot %s %+v", protocol, snapshot)
			}
			minimum := ((int64(len(requestBody))+64)*tc.minimumInputRate + 100*tc.minimumOutputRate) * 1000
			if attempt.Quote.Amount < minimum {
				t.Fatalf("reservation omitted costly TTL/tier/reasoning: %d < %d", attempt.Quote.Amount, minimum)
			}
			// An administrator activates a new price after reserve. Settlement must
			// retain both the exact old price and its immutable content version.
			*tc.pricing.InputCacheMissPrice = 999
			*tc.pricing.OutputPrice = 999
			if config.PricingVersionID(tc.pricing) == snapshot.VersionID {
				t.Fatal("live price fixture did not change")
			}
			body := &budgetResponseBody{capture: &usageCapture{ReadCloser: io.NopCloser(strings.NewReader(tc.raw))}, attempt: attempt, pricing: snapshot, protocol: protocol, status: 200}
			if _, err := io.Copy(io.Discard, body); err != nil {
				t.Fatal(err)
			}
			if err := body.Close(); err != nil {
				t.Fatal(err)
			}
			session.ParentDone()
			store, _ := manager.Store()
			state, err := store.Snapshot(context.Background(), policy, time.Now())
			if err != nil || state.Reserved != 0 || state.Spent < tc.want || state.Spent > tc.want+1 {
				t.Fatalf("typed snapshot charge %+v want %d..%d err=%v", state, tc.want, tc.want+1, err)
			}
			records, err := store.Attempts(context.Background(), "logical")
			if err != nil || len(records) != 1 || records[0].PricingVersion != snapshot.VersionID || records[0].UsageSource != "provider" {
				t.Fatalf("snapshot audit %+v %v", records, err)
			}
		})
	}
}
