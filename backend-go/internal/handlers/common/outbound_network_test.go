package common

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/httpclient"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/warmup"
)

func TestLocalEgressFailureSkipsKeyRotationAndUpstreamPenalties(t *testing.T) {
	policy := &config.OutboundNetworkConfig{InterfaceName: "yunqi-missing", DNS: config.OutboundDNSConfig{Mode: "bound", Servers: []string{"127.0.0.1:53"}}}
	cm, sched, mm, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{Name: "egress", BaseURL: "http://127.0.0.1:1", APIKeys: []string{"key1", "key2"}, OutboundNetwork: policy, Status: "active", ServiceType: "openai"})
	defer cleanup()
	cfg := cm.GetConfig()
	up := &cfg.Upstream[0]
	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m"}`)))
	selected, penalties := 0, 0
	handled, _, _, fail, _, err := TryUpstreamWithAllKeys(c, config.NewEnvConfig(), cm, sched, scheduler.ChannelKindMessages, "Messages", mm, up, []warmup.URLLatencyResult{{URL: up.BaseURL}}, []byte(`{"model":"m"}`), nil, false,
		func(*config.UpstreamConfig, map[string]bool) (string, error) { selected++; return "key1", nil },
		func(fiber.Ctx, *config.UpstreamConfig, string) (*http.Request, error) {
			t.Fatal("must reject policy before building upstream request")
			return nil, nil
		},
		func(string) { penalties++ }, func(string) { penalties++ }, func(string) {},
		func(fiber.Ctx, *http.Response, *config.UpstreamConfig, string, []byte) (*types.Usage, error) {
			return nil, nil
		}, "m", "", 0, nil)
	if handled || fail != nil || !httpclient.IsEgressError(err) || selected != 0 || penalties != 0 {
		t.Fatalf("handled=%t err=%v selected=%d penalties=%d", handled, err, selected, penalties)
	}
	HandleAllChannelsFailed(c, nil, err, "Messages")
	if w.Code != 503 || !strings.Contains(w.Body.String(), "network_egress_unavailable") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRacingLocalEgressFailureDoesNotHideUpstreamSiblingFailure(t *testing.T) {
	local := &httpclient.EgressError{Stage: "bind", Err: errors.New("denied")}
	runs := &racingRuns{results: map[int]MultiChannelAttemptResult{1: {LastError: local}, 2: {LastError: context.Canceled}}}
	if runs.hasNonEgressFailure() {
		t.Fatal("local failure / cancellation treated as upstream error")
	}
	runs.results[3] = MultiChannelAttemptResult{LastError: errors.New("upstream refused")}
	if !runs.hasNonEgressFailure() {
		t.Fatal("real upstream sibling failure hidden")
	}
}
