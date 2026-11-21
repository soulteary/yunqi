package common

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/telemetry"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/warmup"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDiagnosticActualKeyFailoverAndLocalBuildRejection(t *testing.T) {
	for _, buildFail := range []bool{false, true} {
		t.Run(map[bool]string{false: "key-failover", true: "local-build"}[buildFail], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-request-id", "provider-id")
				if calls.Add(1) == 1 {
					w.WriteHeader(429)
					_, _ = w.Write([]byte(`{"error":{"message":"rate limit exceeded"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer server.Close()
			cm, sch, mm, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{Name: "diagnostic", ChannelUID: "ch_diag", BaseURL: server.URL, APIKeys: []string{"secret-one", "secret-two"}, Status: "active", ServiceType: "openai"})
			defer cleanup()
			registry := telemetry.NewRegistry()
			app := httpbridge.NewApp()
			app.Use(registry.Middleware())
			app.Post("/v1/messages", func(c fiber.Ctx) error {
				telemetry.LinkInternal(httpbridge.Request(c).Context(), "srv_diagonal", "")
				body := []byte(`{"model":"m","messages":[]}`)
				up := cm.GetConfig().Upstream[0]
				handled, _, _, _, _, err := TryUpstreamWithAllKeys(c, config.NewEnvConfig(), cm, sch, scheduler.ChannelKindMessages, "Messages", mm, &up, []warmup.URLLatencyResult{{URL: server.URL, OriginalIdx: 0}}, body, nil, false,
					func(up *config.UpstreamConfig, failed map[string]bool) (string, error) {
						return cm.GetNextAPIKey(up, failed, "Messages")
					},
					func(c fiber.Ctx, up *config.UpstreamConfig, key string) (*http.Request, error) {
						if buildFail {
							return nil, errors.New("private arbitrary builder error")
						}
						return http.NewRequestWithContext(httpbridge.Request(c).Context(), "POST", up.BaseURL, bytes.NewReader(body))
					}, func(string) {}, nil, nil,
					func(c fiber.Ctx, resp *http.Response, up *config.UpstreamConfig, key string, body []byte) (*types.Usage, error) {
						_ = resp.Body.Close()
						httpbridge.Text(c, 200, "ok")
						return nil, nil
					}, "m", "", 0, sch.GetChannelLogStore(scheduler.ChannelKindMessages))
				if !buildFail && (!handled || err != nil) {
					t.Errorf("failover failed: handled=%v err=%v", handled, err)
				}
				return nil
			})
			w := httptest.NewRecorder()
			httpbridge.Handler(app).ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"m","messages":[]}`)))
			value, ok := registry.LookupDiagnostic(w.Header().Get("X-Yunqi-Request-Id"))
			if !ok {
				t.Fatal("no diagnostics")
			}
			if buildFail {
				if calls.Load() != 0 || len(value.Attempts) != 1 || value.Attempts[0].ErrorSource != "gateway" {
					t.Fatalf("bad local source %+v", value)
				}
			} else {
				if calls.Load() != 2 || len(value.Attempts) != 2 || value.Attempts[0].KeyUID == value.Attempts[1].KeyUID || value.Attempts[0].StatusCode != 429 || value.Attempts[0].ErrorSource != "upstream" || value.Attempts[1].Status != "completed" || value.Attempts[1].UpstreamRequestID != "provider-id" {
					t.Fatalf("bad multi-key chain %+v", value)
				}
			}
			raw, _ := json.Marshal(value)
			for _, private := range []string{"secret-one", "secret-two", "private arbitrary", server.URL} {
				if strings.Contains(string(raw), private) {
					t.Fatal("private diagnostic value", private)
				}
			}
		})
	}
}

func TestDiagnosticCompletionSurvivesShortChannelRing(t *testing.T) {
	registry := telemetry.NewRegistry()
	app := httpbridge.NewApp()
	app.Use(registry.Middleware())
	store := metrics.NewChannelLogStore()
	app.Post("/v1/messages", func(c fiber.Ctx) error {
		d := telemetry.DiagnosticsFromContext(httpbridge.Request(c).Context())
		id := CreatePendingLog(store, "bucket", 0, "ch", "m", "", "", "", "key", "url", "Messages", "", "proxy", nil, "", func(l *metrics.ChannelLog) { l.Diagnostics = d; l.ChannelUID = "ch"; l.KeyUID = "key_uid" })
		for i := 0; i < 55; i++ {
			other := CreatePendingLog(store, "bucket", 0, "ch", "m", "", "", "", "key", "url", "Messages", "", "proxy", nil, "")
			CompleteLog(store, "bucket", other, 200, true, "", false)
		}
		CompleteLog(store, "bucket", id, 429, false, "rate limited", false)
		return nil
	})
	w := httptest.NewRecorder()
	httpbridge.Handler(app).ServeHTTP(w, httptest.NewRequest("POST", "/v1/messages", nil))
	v, ok := registry.LookupDiagnostic(w.Header().Get("X-Yunqi-Request-Id"))
	if !ok || len(v.Attempts) != 1 || v.Attempts[0].Status != "failed" || v.Attempts[0].StatusCode != 429 {
		t.Fatalf("final state lost after UI eviction %+v", v)
	}
}
