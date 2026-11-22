package common

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/utils"
	"github.com/soulteary/yunqi/internal/warmup"
)

// Exercise the production wrapper ordering with drain and budgets both disabled.
// Explicit Accept-Encoding prevents net/http's automatic gzip decompression from
// hiding a capture installed before the protocol handler's own decompressor.
func TestUsageCaptureAfterGzipDecompression(t *testing.T) {
	for _, tc := range []struct {
		name, wire      string
		stream, invalid bool
	}{
		{"json", `{"usage":{"prompt_tokens":7,"completion_tokens":0}}`, false, false},
		{"sse", "data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":0}}\n\ndata: [DONE]\n\n", true, false},
		{"invalid-json-gzip", "not a gzip stream", false, true},
		{"invalid-sse-gzip", "not a gzip stream", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, callbacks := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Accept-Encoding") != "gzip" {
					t.Error("missing explicit encoding")
				}
				w.Header().Set("Content-Encoding", "gzip")
				if tc.stream {
					w.Header().Set("Content-Type", "text/event-stream")
				} else {
					w.Header().Set("Content-Type", "application/json")
				}
				if tc.invalid {
					_, _ = io.WriteString(w, tc.wire)
					return
				}
				z := gzip.NewWriter(w)
				_, _ = io.WriteString(z, tc.wire)
				_ = z.Close()
			}))
			defer server.Close()
			cm, sch, mm, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{Name: "gzip", ChannelUID: "ch_gzip", BaseURL: server.URL, APIKeys: []string{"test-key"}, Status: "active", ServiceType: "openai"})
			defer cleanup()
			app := httpbridge.NewApp()
			app.Post("/v1/messages", func(c fiber.Ctx) error {
				body := []byte(`{"model":"m","messages":[]}`)
				up := cm.GetConfig().Upstream[0]
				handled, key, _, _, usage, err := TryUpstreamWithAllKeys(c, config.NewEnvConfig(), cm, sch, scheduler.ChannelKindMessages, "Messages", mm, &up, []warmup.URLLatencyResult{{URL: server.URL, OriginalIdx: 0}}, body, nil, tc.stream,
					func(up *config.UpstreamConfig, failed map[string]bool) (string, error) {
						return cm.GetNextAPIKey(up, failed, "Messages")
					},
					func(c fiber.Ctx, up *config.UpstreamConfig, key string) (*http.Request, error) {
						req, err := http.NewRequestWithContext(httpbridge.Request(c).Context(), "POST", up.BaseURL+"/chat/completions", bytes.NewReader(body))
						if err == nil {
							req.Header.Set("Accept-Encoding", "gzip")
						}
						return req, err
					}, func(string) {}, nil, nil,
					func(c fiber.Ctx, resp *http.Response, up *config.UpstreamConfig, key string, body []byte) (*types.Usage, error) {
						callbacks++
						if err := utils.DecompressResponseBodyIfNeeded(resp); err != nil {
							return nil, err
						}
						defer resp.Body.Close()
						wire, err := io.ReadAll(resp.Body)
						if err != nil {
							return nil, err
						}
						if string(wire) != tc.wire {
							t.Errorf("wire changed: %q", wire)
						}
						return &types.Usage{InputTokens: 7, OutputTokens: 0}, nil
					}, "m", "", 0, sch.GetChannelLogStore(scheduler.ChannelKindMessages))
				if tc.invalid {
					if err == nil || key != "" || callbacks != 0 {
						t.Errorf("invalid compression accepted: key=%q callbacks=%d err=%v", key, callbacks, err)
					}
				} else if !handled || err != nil || usage == nil || usage.Provenance == nil || usage.Provenance.UsageSource != "provider" || len(usage.Provenance.RawUsage) == 0 || usage.OutputTokens != 0 {
					t.Errorf("lost provider accounting: handled=%v usage=%+v err=%v", handled, usage, err)
				}
				return nil
			})
			httpbridge.Handler(app).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(nil)))
			if calls != 1 {
				t.Fatalf("unexpected replay: %d calls", calls)
			}
		})
	}
}
