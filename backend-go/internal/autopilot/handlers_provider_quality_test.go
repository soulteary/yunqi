package autopilot

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestHandleProviderQualityProbeRejectsInvalidRepetitions(t *testing.T) {

	probe, _, endpoint := newProviderQualityProbeFixture(t, "https://example.invalid", "sk-handler-test", 3)
	router := httpbridge.NewApp()
	router.Post("/api/health-center/provider-quality/probe", handleProviderQualityProbe(probe))

	body := `{"endpointUid":"` + endpoint.EndpointUID + `","modelId":"test-model","repetitions":4}`
	req := httptest.NewRequest(http.MethodPost, "/api/health-center/provider-quality/probe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	httpbridge.Handler(router).ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"code":"invalid_repetitions"`) {
		t.Fatalf("body=%s", resp.Body.String())
	}
	if state := probe.BudgetState(); state.Used != 0 {
		t.Fatalf("无效请求不应消耗预算: %+v", state)
	}
}

func TestHandleProviderQualityProbeNilProbe(t *testing.T) {

	router := httpbridge.NewApp()
	router.Post("/probe", handleProviderQualityProbe(nil))
	req := httptest.NewRequest(http.MethodPost, "/probe", strings.NewReader(`{}`))
	resp := httptest.NewRecorder()
	httpbridge.Handler(router).ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}
