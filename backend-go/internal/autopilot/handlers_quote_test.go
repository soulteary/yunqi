package autopilot

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallerQuoteUsesChannelPriceWithoutDisclosingInfrastructure(t *testing.T) {
	cfg := baseTestConfig()
	price := 2.0
	for i := range cfg.Upstream {
		cfg.Upstream[i].ModelCapabilities = map[string]config.UpstreamModelCapability{"quote-model": {ContextWindowTokens: 2000000, MaxOutputTokens: 1000000, Pricing: &config.ModelPricing{InputCacheMissPrice: &price, OutputPrice: &price}}}
	}
	sch, cm, cleanup := createTestScheduler(t, cfg)
	defer cleanup()
	app := httpbridge.NewApp()
	app.Post("/v1/quote", CallerQuoteHandler(cm, sch))
	for _, tc := range []struct {
		body     string
		status   int
		complete bool
	}{
		{`{"channelKind":"messages","model":"quote-model","inputTokens":1000000,"outputTokens":500000}`, 200, true},
		{`{"channelKind":"messages","model":"quote-model","inputTokens":1000000}`, 200, false},
		{`{"channelKind":"messages","model":"quote-model","inputTokens":-1}`, 400, false},
		{`{"channelKind":"other","model":"quote-model"}`, 400, false},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/quote", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		httpbridge.Handler(app).ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		if tc.status != 200 {
			continue
		}
		var out struct {
			EstimatedCost   float64 `json:"estimatedCost"`
			PricingComplete bool    `json:"pricingComplete"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.PricingComplete != tc.complete {
			t.Fatalf("%s", w.Body.String())
		}
		if tc.complete && out.EstimatedCost != 3 {
			t.Fatalf("wrong channel price: %s", w.Body.String())
		}
		for _, secret := range []string{"premium", "example.com", "sk-", "channelIndex", "outbound"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatalf("quote disclosed %s", secret)
			}
		}
	}
}

func TestCallerQuoteRejectsMalformedBodyBudgetsAndFlagsUnknownInputMedia(t *testing.T) {
	cfg := baseTestConfig()
	sch, cm, cleanup := createTestScheduler(t, cfg)
	defer cleanup()
	app := httpbridge.NewApp()
	app.Post("/v1/quote", CallerQuoteHandler(cm, sch))
	for _, raw := range []string{`"foo"`, `1.5`, `1e99`, `9223372036854775808`, `-1`, `true`, `{}`, `[]`} {
		body := `{"channelKind":"messages","model":"claude-sonnet-4","body":{"max_tokens":` + raw + `}}`
		req := httptest.NewRequest("POST", "/v1/quote", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("budget %s status=%d body=%s", raw, w.Code, w.Body.String())
		}
	}
	for _, body := range []string{
		`{"channelKind":"messages","model":"claude-sonnet-4","outputTokens":10}`,
		`{"channelKind":"messages","model":"claude-sonnet-4","inputTokens":10,"outputTokens":10,"body":{"messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AA","format":"wav"}}]}]}}`,
		`{"channelKind":"messages","model":"claude-sonnet-4","inputTokens":10,"outputTokens":10,"body":{"modalities":["audio"]}}`,
	} {
		req := httptest.NewRequest("POST", "/v1/quote", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%d: %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), `"pricingComplete":true`) {
			t.Fatalf("false complete quote: %s", w.Body.String())
		}
	}
}
