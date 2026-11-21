package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/telemetry"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestTimelineRouteRequiresGlobalAdmin(t *testing.T) {
	a := newRouteTestApplication(t, true, "production")
	r := a.newRouter()

	// Route-level middleware populates a private local snapshot; no upstream is used.
	app := httpbridge.NewApp()
	app.Use(a.monitoring.Middleware())
	app.Post("/v1/responses", func(c fiber.Ctx) error {
		telemetry.LinkInternal(httpbridge.Request(c).Context(), "srv_timeline", "")
		return nil
	})
	w := httptest.NewRecorder()
	httpbridge.Handler(app).ServeHTTP(w, httptest.NewRequest("POST", "/v1/responses", nil))
	id := w.Header().Get("X-Yunqi-Request-Id")
	for _, tc := range []struct {
		key  string
		code int
	}{{"", 401}, {"route-proxy", 401}, {"route-admin", 200}} {
		req := httptest.NewRequest("GET", "/api/reports/request-timeline/"+id, nil)
		if tc.key != "" {
			req.Header.Set("x-api-key", tc.key)
		}
		out := httptest.NewRecorder()
		httpbridge.Handler(r).ServeHTTP(out, req)
		if out.Code != tc.code {
			t.Fatalf("key %q code=%d body=%s", tc.key, out.Code, out.Body.String())
		}
		if tc.code == 200 && (!strings.Contains(out.Body.String(), "srv_timeline") || out.Header().Get("Cache-Control") != "no-store") {
			t.Fatal(out.Body.String())
		}
	}
}
