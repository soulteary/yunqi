package common

import (
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/scheduler"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestStrictConcurrentRequestsBindBeforeAnyResponse(t *testing.T) {
	cm, sch, _, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{Name: "a", ChannelUID: "a", BaseURL: "https://a.invalid", APIKeys: []string{"key-a"}, Status: "active", Priority: 1})
	defer cleanup()
	defer sch.GetTraceAffinityManager().Stop()
	if err := cm.AddUpstream(config.UpstreamConfig{Name: "b", ChannelUID: "b", BaseURL: "https://b.invalid", APIKeys: []string{"key-b"}, Status: "active", Priority: 2}); err != nil {
		t.Fatal(err)
	}
	env := &config.EnvConfig{StrictSessionAffinity: true}
	entered := make(chan string, 24)
	release := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			c := strictTestContext(w)
			httpbridge.SetValue(c, middleware.ContextKeyPrincipalID, "principal-a")
			HandleMultiChannelFailover(c, env, sch, scheduler.ChannelKindMessages, "Messages", "same-session", "m", false, "main", func(c fiber.Ctx, s *scheduler.SelectionResult) MultiChannelAttemptResult {
				entered <- s.Route.ChannelUID
				<-release
				httpbridge.Text(c, 200, "ok")
				return MultiChannelAttemptResult{Handled: true, SuccessKey: "key"}
			}, nil, nil)
			if w.Code != 200 || w.Header().Get("X-Yunqi-Session-Affinity") != "strict" {
				t.Errorf("strict request failed: %d %s", w.Code, w.Body.String())
			}
		}()
	}
	var bound string
	for i := 0; i < 24; i++ {
		uid := <-entered
		if bound == "" {
			bound = uid
		}
		if uid != bound {
			t.Error("first requests split", bound, uid)
		}
	}
	close(release)
	wg.Wait()
	w := httptest.NewRecorder()
	c := strictTestContext(w)
	httpbridge.SetValue(c, middleware.ContextKeyPrincipalID, "principal-a")
	calls := 0
	HandleMultiChannelFailover(c, env, sch, scheduler.ChannelKindMessages, "Messages", "same-session", "m", false, "main", func(c fiber.Ctx, s *scheduler.SelectionResult) MultiChannelAttemptResult {
		calls++
		if s.Route.ChannelUID != bound {
			t.Error("migrated")
		}
		return MultiChannelAttemptResult{LastError: errors.New("fake upstream failure")}
	}, nil, func(c fiber.Ctx, _ *FailoverError, _ error) { httpbridge.Text(c, 503, "strict failure") })
	if calls != 1 || w.Code != 503 {
		t.Fatalf("strict failover escaped: calls=%d status=%d", calls, w.Code)
	}
	w = httptest.NewRecorder()
	c = strictTestContext(w)
	HandleMultiChannelFailover(c, env, sch, scheduler.ChannelKindMessages, "Messages", "same-session", "m", false, "main", func(fiber.Ctx, *scheduler.SelectionResult) MultiChannelAttemptResult {
		t.Fatal("missing principal admitted")
		return MultiChannelAttemptResult{}
	}, nil, nil)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func strictTestContext(w *httptest.ResponseRecorder) fiber.Ctx {
	c, _ := httpbridge.TestContext(w)
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("Session_id", "same-session")
	httpbridge.SetRequest(c, req)
	return c
}
