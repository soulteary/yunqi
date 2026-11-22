package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/soulteary/yunqi/internal/autopilot"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/eventbus"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/scheduler"
)

func newRouteTestApplication(t *testing.T, web bool, env string) *application {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"upstream":[],"chatUpstream":[],"responsesUpstream":[],"geminiUpstream":[],"imagesUpstream":[],"vectorsUpstream":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewConfigManager(cfgPath, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cfg.Close() })
	a := &application{cfgManager: cfg, envCfg: &config.EnvConfig{Env: env, EnableWebUI: web, ProxyAccessKey: "route-proxy", AdminAccessKey: "route-admin", MaxRequestBodySize: 1024 * 1024}}
	a.messagesMetricsManager = metrics.NewMetricsManagerWithConfig(20, 0.7)
	a.responsesMetricsManager = metrics.NewMetricsManagerWithConfig(20, 0.7)
	a.geminiMetricsManager = metrics.NewMetricsManagerWithConfig(20, 0.7)
	a.chatMetricsManager = metrics.NewMetricsManagerWithConfig(20, 0.7)
	a.imagesMetricsManager = metrics.NewMetricsManagerWithConfig(20, 0.7)
	a.vectorsMetricsManager = metrics.NewMetricsManagerWithConfig(20, 0.7)
	for _, m := range []*metrics.MetricsManager{a.messagesMetricsManager, a.responsesMetricsManager, a.geminiMetricsManager, a.chatMetricsManager, a.imagesMetricsManager, a.vectorsMetricsManager} {
		t.Cleanup(m.Stop)
	}
	a.channelScheduler = scheduler.NewChannelScheduler(cfg, a.messagesMetricsManager, a.responsesMetricsManager, a.geminiMetricsManager, a.chatMetricsManager, a.imagesMetricsManager, nil, nil, a.vectorsMetricsManager)
	store, err := autopilot.NewProfileStore(filepath.Join(t.TempDir(), "autopilot.db"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := autopilot.NewManager(store, autopilot.NewMetricsManagerAdapter(a.messagesMetricsManager), cfg, autopilot.ManagerConfig{})
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	a.autopilotManager = manager
	trace, err := autopilot.NewTraceStoreWithDB(store.DB())
	if err != nil {
		t.Fatal(err)
	}
	manager.SetTraceStore(trace)
	manager.SetSmartRouter(autopilot.NewSmartRouter(store, manager.ManualIntentStore(), trace, cfg))
	abStore, err := autopilot.NewABTestStoreWithDB(store.DB())
	if err != nil {
		t.Fatal(err)
	}
	manager.SetABTestStore(abStore)
	manager.SetABTestSampler(autopilot.NewABTestSampler(abStore, func() autopilot.ABTestSamplerConfig { return autopilot.ABTestSamplerConfig{} }))
	a.stateEventBus = eventbus.NewBus()
	t.Cleanup(func() {
		if a.newApiSyncService != nil {
			a.newApiSyncService.Stop()
		}
		if a.autoDiscoveryRunner != nil {
			a.autoDiscoveryRunner.Stop()
		}
	})
	return a
}

// The shared manifest and mode-specific additions were captured from the actual
// router before splitting main.go; keep one base list rather than duplicate routes.
// They include optional Autopilot, trace, A/B, discovery and prefixed proxy routes.
func TestRouteManifestCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name        string
		web         bool
		env         string
		extraRoutes []string
	}{
		{"production-web", true, "production", []string{"GET /assets/*", "HEAD /assets/*"}},
		{"production-api", false, "production", nil},
		{"development-web", true, "development", []string{"GET /assets/*", "HEAD /assets/*", "GET /admin/dev/info"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newRouteTestApplication(t, tc.web, tc.env)
			router := a.newRouter()
			var got []string
			for _, route := range router.GetRoutes(true) {
				got = append(got, route.Method+" "+route.Path)
			}
			sort.Strings(got)
			data, err := os.ReadFile(filepath.Join("testdata", "routes", "common.txt"))
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Split(strings.TrimSpace(string(data)), "\n")
			want = append(want, tc.extraRoutes...)
			for i := range want {
				want[i] = regexp.MustCompile(`\*(rest|modelAction|filepath)`).ReplaceAllString(want[i], "*")
			}
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("route surface changed\ngot (%d):\n%s\nwant (%d):\n%s", len(got), strings.Join(got, "\n"), len(want), strings.Join(want, "\n"))
			}
			if a.autoDiscoveryRunner != nil || a.newApiSyncService != nil || a.scheduledRecoveryStop != nil {
				t.Fatal("router registration must not create or start background workers")
			}
		})
	}
}

func TestRouterAuthenticationAndFrontendCompatibility(t *testing.T) {
	a := newRouteTestApplication(t, true, "production")
	router := a.newRouter()
	for _, tc := range []struct {
		method, path, key string
		status            int
	}{
		{"GET", "/health", "", 200},
		{"GET", "/team/health", "", 200},
		{"GET", "/console", "", 200},
		{"GET", "/api/network/interfaces", "", 401},
		{"GET", "/api/metrics/prometheus", "", 401},
		{"GET", "/api/metrics/prometheus", "route-proxy", 401},
		{"GET", "/api/metrics/prometheus", "route-admin", 200},
		{"GET", "/api/reports/requests?since=0&until=1", "", 401},
		{"GET", "/api/logging/body", "", 401},
		{"GET", "/api/logging/body/export", "", 401},
		{"PUT", "/api/logging/body", `{"requestBodies":true}`, 401},
		{"GET", "/api/network/interfaces", "route-proxy", 401},
		{"GET", "/api/network/interfaces", "route-admin", 200},
		{"POST", "/api/network/diagnose", "route-proxy", 401},
		{"POST", "/api/network/diagnose", "route-admin", 400},
		{"GET", "/api/console/status", "", 401},
		{"GET", "/api/console/status", "route-proxy", 401},
		{"GET", "/api/console/status", "route-admin", 200},
		{"GET", "/api/console/codex-auto-review", "", 401},
		{"GET", "/api/console/codex-auto-review", "route-proxy", 401},
		{"GET", "/api/console/codex-auto-review", "route-admin", 200},
		{"PUT", "/api/console/codex-auto-review", "route-proxy", 401},
		{"PUT", "/api/console/codex-auto-review", "route-admin", 400},
		{"GET", "/api/messages/channels", "route-admin", 200},
		{"GET", "/api/channels", "route-admin", 200},
		{"GET", "/v1/models", "", 401},
		{"GET", "/team/v1/models", "", 401},
		{"POST", "/v1/messages", "", 401},
		{"POST", "/team/v1/messages", "", 401},
		{"POST", "/v1/messages/count_tokens", "", 401},
		{"POST", "/v1/chat/completions", "", 401},
		{"POST", "/team/v1/chat/completions", "", 401},
		{"POST", "/v1/responses", "", 401},
		{"POST", "/team/v1/responses", "", 401},
		{"GET", "/v1/responses", "", 405},
		{"GET", "/team/v1/responses", "", 405},
		{"POST", "/v1/responses/compact", "", 401},
		{"POST", "/v1/alpha/history/v2/test", "", 401},
		{"POST", "/team/v1/alpha/notes/v2/test", "", 401},
		{"POST", "/v1beta/models/test:generateContent", "", 401},
		{"POST", "/team/v1beta/models/test:streamGenerateContent", "", 401},
		{"POST", "/v1/images/generations", "", 401},
		{"POST", "/v1/images/edits", "", 401},
		{"POST", "/v1/images/variations", "", 401},
		{"POST", "/team/v1/images/edits", "", 401},
		{"POST", "/v1/embeddings", "", 401},
		{"POST", "/team/v1/embeddings", "", 401},
	} {
		t.Run(tc.method+" "+tc.path+" "+tc.key, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.key != "" {
				req.Header.Set("x-api-key", tc.key)
			}
			recorder := httptest.NewRecorder()
			httpbridge.Handler(router).ServeHTTP(recorder, req)
			if recorder.Code != tc.status {
				t.Fatalf("status=%d, want=%d; body=%s", recorder.Code, tc.status, recorder.Body.String())
			}
			if tc.path == "/console" && !strings.Contains(recorder.Header().Get("Content-Type"), "text/html") {
				t.Fatal("SPA route must still serve HTML")
			}
		})
	}
}

func TestAPIOnlyManagementBoundary(t *testing.T) {
	a := newRouteTestApplication(t, false, "production")
	router := a.newRouter()
	for _, path := range []string{"/", "/console", "/api/console/status"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("x-api-key", "route-admin")
		recorder := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(recorder, req)
		if recorder.Code != 404 {
			t.Fatalf("%s status=%d, want=404", path, recorder.Code)
		}
	}
}
