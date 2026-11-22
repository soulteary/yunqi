package main

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/entrybudget"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestPrincipalBudgetStreamModeCannotBeDowngradedByContentType(t *testing.T) {
	zeroJSON := `{"id":"zero","object":"chat.completion","model":"mime-budget-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0}}`
	zeroSSE := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0}}\n\ndata: [DONE]\n\n"
	for _, tc := range []struct {
		name, mime, wire    string
		stream, known, gzip bool
	}{
		{"stream-json-no-terminal", "application/json", zeroJSON, true, false, false},
		{"stream-text-no-terminal", "text/plain", zeroJSON, true, false, false},
		{"stream-wrong-mime-terminal-zero", "application/json", zeroSSE, true, true, false},
		{"nonstream-json-zero", "application/json", zeroJSON, false, true, false},
		{"nonstream-sse-bridge-zero", "text/event-stream", zeroSSE, false, true, false},
		{"gzip-stream-json-no-terminal", "application/json", zeroJSON, true, false, true},
		{"gzip-stream-wrong-mime-terminal-zero", "application/json", zeroSSE, true, true, true},
		{"gzip-nonstream-json-zero", "application/json", zeroJSON, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", tc.mime)
				if tc.gzip {
					if r.Header.Get("Accept-Encoding") != "gzip" {
						t.Error("explicit encoding not forwarded")
					}
					w.Header().Set("Content-Encoding", "gzip")
					encoded := gzip.NewWriter(w)
					_, _ = io.WriteString(encoded, tc.wire)
					_ = encoded.Close()
				} else {
					_, _ = io.WriteString(w, tc.wire)
				}
			}))
			defer upstream.Close()
			app := newRouteTestApplication(t, false, "production")
			app.envCfg.RequestTimeout = 10000
			app.envCfg.BudgetManager = entrybudget.NewManager(filepath.Join(t.TempDir(), "budget.db"))
			defer app.envCfg.BudgetManager.Close()
			price := 1.0
			if err := app.cfgManager.AddChatUpstream(config.UpstreamConfig{ChannelUID: "budget_mime", Name: "budget-mime", BaseURL: upstream.URL, APIKeys: []string{"fake-provider"}, ServiceType: "openai", Status: "active", CustomHeaders: map[string]string{"Accept-Encoding": "gzip"}, SupportedModels: []string{"mime-budget-model"}, ModelCapabilities: map[string]config.UpstreamModelCapability{"mime-budget-model": {ContextWindowTokens: 4096, MaxOutputTokens: 100, Pricing: &config.ModelPricing{InputCacheMissPrice: &price, OutputPrice: &price, InputCacheHitPrice: &price}}}}); err != nil {
				t.Fatal(err)
			}
			if err := app.cfgManager.PutEntryKey(config.EntryKey{ID: "mime-key", KeyHash: config.EntryKeyHash("route-proxy"), PrincipalID: "mime-principal"}, ""); err != nil {
				t.Fatal(err)
			}
			policy := entrybudget.Policy{PrincipalID: "mime-principal", Period: "month", Timezone: "UTC", LimitNanoUSD: 1_000_000_000, RequestCapNanoUSD: 1_000_000_000}
			if err := app.cfgManager.PutPrincipalBudget(policy); err != nil {
				t.Fatal(err)
			}
			stream := "false"
			if tc.stream {
				stream = "true"
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"mime-budget-model","stream":`+stream+`,"max_tokens":10,"messages":[{"role":"user","content":"hello"}]}`))
			request.Header.Set("x-api-key", "route-proxy")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			httpbridge.Handler(app.newRouter()).ServeHTTP(response, request)
			if calls != 1 {
				t.Fatalf("unexpected physical attempts=%d status=%d body=%s", calls, response.Code, response.Body.String())
			}
			store, err := app.envCfg.BudgetManager.Store()
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := store.Snapshot(context.Background(), policy, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			records, err := store.Requests(context.Background(), policy.PrincipalID, 10)
			if err != nil || len(records) != 1 {
				t.Fatalf("missing real reservation: %v %+v", err, records)
			}
			attempts, err := store.Attempts(context.Background(), records[0].ID)
			if err != nil || len(attempts) != 1 || attempts[0].QuotedNanoUSD <= 0 {
				t.Fatalf("no actual predebit: %v %+v", err, attempts)
			}
			if snapshot.Reserved != 0 {
				t.Fatalf("request did not settle: %+v", snapshot)
			}
			if tc.known {
				if response.Code != 200 || snapshot.Spent != 0 || attempts[0].ChargedNanoUSD != 0 || attempts[0].UsageSource != "provider" {
					t.Fatalf("authoritative zero did not release hold: status=%d snapshot=%+v attempts=%+v body=%s", response.Code, snapshot, attempts, response.Body.String())
				}
			} else {
				if snapshot.Spent <= 0 || attempts[0].ChargedNanoUSD != attempts[0].QuotedNanoUSD || attempts[0].UsageSource != "unknown" || attempts[0].Reason != "incomplete_or_missing_provider_usage" {
					t.Fatalf("MIME bypassed source terminal: snapshot=%+v attempts=%+v", snapshot, attempts)
				}
			}
		})
	}
}
