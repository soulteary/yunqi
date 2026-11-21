package common

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/converters"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/warmup"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFinalWireConversionReportsCacheLossWithoutChangingBody(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	raw := []byte(`{"messages":[],"model":"m"}`)
	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/messages", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := recordFinalConversion(c, []byte(`{"prompt_cache_options":{"prewarm":false}}`), req, "Responses", "claude"); err != nil {
		t.Fatal(err)
	}
	var report converters.ConversionReport
	if err := json.Unmarshal([]byte(w.Header().Get("X-Yunqi-Conversion")), &report); err != nil {
		t.Fatal(err)
	}
	if report.Quality != "lossy" || report.Diagnostics[0].Field != "prompt_cache_options" {
		t.Fatalf("report %#v", report)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, raw) {
		t.Fatalf("audit consumed request body: %s", body)
	}
}

func TestFinalWireConversionRejectsSilentSchemaLoss(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	err := recordFinalConversion(c, []byte(`{"text":{"format":{"type":"json_schema","schema":{"type":"object"}}}}`), req, "Responses", "openai")
	if err == nil {
		t.Fatal("critical schema loss reached upstream")
	}
	if !strings.Contains(w.Header().Get("X-Yunqi-Conversion"), "rejected") {
		t.Fatalf("missing machine-readable rejection: %v", w.Header())
	}
}

func TestExecutorRejectsLossyFinalWireBeforeSending(t *testing.T) {
	cm, sched, mm, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{Name: "conversion-audit", BaseURL: "http://127.0.0.1:1", APIKeys: []string{"test"}, Status: "active", ServiceType: "openai"})
	defer cleanup()
	up := &cm.GetConfig().Upstream[0]
	raw := []byte(`{"model":"m","messages":[],"output_config":{"format":{"type":"json_schema","schema":{"type":"object","additionalProperties":false}}}}`)
	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(raw)))
	penalties := 0
	handled, _, _, fail, _, err := TryUpstreamWithAllKeys(c, config.NewEnvConfig(), cm, sched, scheduler.ChannelKindMessages, "Messages", mm, up, []warmup.URLLatencyResult{{URL: up.BaseURL}}, raw, nil, false,
		func(*config.UpstreamConfig, map[string]bool) (string, error) { return "test", nil },
		func(fiber.Ctx, *config.UpstreamConfig, string) (*http.Request, error) {
			return http.NewRequest(http.MethodPost, up.BaseURL+"/v1/chat/completions", strings.NewReader(`{"model":"m","messages":[]}`))
		},
		func(string) { penalties++ }, func(string) { penalties++ }, func(string) {},
		func(fiber.Ctx, *http.Response, *config.UpstreamConfig, string, []byte) (*types.Usage, error) {
			t.Fatal("lossy request reached upstream")
			return nil, nil
		}, "m", "", 0, nil)
	var loss *converters.ConversionLossError
	if handled || fail != nil || !errors.As(err, &loss) || penalties != 0 {
		t.Fatalf("handled=%v fail=%v err=%v penalties=%d", handled, fail, err, penalties)
	}
	if !strings.Contains(w.Header().Get("X-Yunqi-Conversion"), "structured_output_mode_lost") {
		t.Fatalf("report missing: %v", w.Header())
	}
}

func TestFinalWireProtocolUsesEndpointSuffixNotBaseURLPrefix(t *testing.T) {
	for _, tc := range []struct{ path, from, target string }{
		{"/responses-proxy/v1/messages", "responses", "messages"},
		{"/chat/completions-proxy/v1/responses", "chat", "responses"},
		{"/messages-proxy/v1/chat/completions", "messages", "chat"},
		{"/responses-proxy/v1beta/models/m:streamGenerateContent", "responses", "gemini"},
	} {
		w := httptest.NewRecorder()
		c, _ := httpbridge.TestContext(w)
		httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/test", nil))
		req, _ := http.NewRequest(http.MethodPost, "https://example.invalid"+tc.path, strings.NewReader(`{}`))
		if err := recordFinalConversion(c, []byte(`{}`), req, tc.from, "responses"); err != nil {
			t.Fatal(err)
		}
		var report converters.ConversionReport
		if err := json.Unmarshal([]byte(w.Header().Get("X-Yunqi-Conversion")), &report); err != nil {
			t.Fatalf("missing report for %s: %v", tc.path, err)
		}
		if report.Steps[0].To != tc.target {
			t.Fatalf("path %s target=%s want=%s", tc.path, report.Steps[0].To, tc.target)
		}
	}
}
