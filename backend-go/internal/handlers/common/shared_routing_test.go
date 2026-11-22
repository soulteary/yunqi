package common

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/entrylease"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
)

func TestSharedCapacityErrorSurvivesCandidateExhaustion(t *testing.T) {
	cm, sch, _, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{Name: "first", ChannelUID: "first", BaseURL: "https://first.invalid", APIKeys: []string{"fake"}, Status: "active"})
	defer cleanup()
	if err := cm.AddUpstream(config.UpstreamConfig{Name: "second", ChannelUID: "second", BaseURL: "https://second.invalid", APIKeys: []string{"fake"}, Status: "active"}); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	c := strictTestContext(response)
	calls := 0
	HandleMultiChannelFailover(c, &config.EnvConfig{}, sch, scheduler.ChannelKindMessages, "Messages", "", "m", false, "main", func(c fiber.Ctx, _ *scheduler.SelectionResult) MultiChannelAttemptResult {
		calls++
		disabled := "disabled"
		for i := 0; i < 2; i++ {
			if _, err := cm.UpdateUpstream(i, config.UpstreamUpdate{Status: &disabled}); err != nil {
				t.Fatal(err)
			}
		}
		return MultiChannelAttemptResult{LastError: &channelAdmissionError{cause: entrylease.ErrConcurrency, retry: 1500 * time.Millisecond}}
	}, nil, func(c fiber.Ctx, _ *FailoverError, _ error) { httpbridge.Text(c, 500, "lost local capacity error") })
	if response.Code != 429 || response.Header().Get("Retry-After") != "2" || calls != 1 {
		t.Fatalf("capacity became upstream failure: status=%d retry=%q calls=%d body=%s", response.Code, response.Header().Get("Retry-After"), calls, response.Body.String())
	}
}
