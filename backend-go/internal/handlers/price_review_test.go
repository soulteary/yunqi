package handlers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/presetstore"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPriceReviewRequiresGlobalAdminAndReturnsActualStateOnConflict(t *testing.T) {
	updater := presetstore.NewPresetUpdater(presetstore.NewPresetStore(nil), presetstore.UpdaterConfig{CacheDir: t.TempDir()})
	for _, tc := range []struct {
		principal middleware.AdminPrincipal
		want      int
	}{
		{middleware.AdminPrincipal{}, 403}, {middleware.AdminPrincipal{ID: "viewer", Role: "viewer"}, 403}, {middleware.AdminPrincipal{ID: "operator", Role: "operator"}, 403}, {middleware.AdminPrincipal{ID: "scoped-admin", Role: "admin", ChannelUIDs: []string{"ch_one"}}, 403}, {middleware.AdminPrincipal{ID: "owner", Role: "admin"}, 200},
	} {
		app := httpbridge.NewApp()
		app.Use(func(c fiber.Ctx) error { middleware.SetAdminPrincipal(c, tc.principal); return c.Next() })
		app.Get("/state", PriceReview(updater, "state"))
		app.Post("/approve", PriceReview(updater, "approve"))
		recorder := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/state", nil))
		if recorder.Code != tc.want {
			t.Fatalf("actor=%s status=%d body=%s", tc.principal.ID, recorder.Code, recorder.Body.String())
		}
		if tc.want == 200 {
			if recorder.Header().Get("Cache-Control") != "no-store" || !strings.Contains(recorder.Body.String(), "activeVersion") {
				t.Fatal("missing safe state feedback")
			}
			conflict := httptest.NewRecorder()
			httpbridge.Handler(app).ServeHTTP(conflict, httptest.NewRequest(http.MethodPost, "/approve", strings.NewReader(`{"reviewId":"stale","allowAnomalies":true}`)))
			if conflict.Code != 409 || !strings.Contains(conflict.Body.String(), `"persisted":false`) || !strings.Contains(conflict.Body.String(), "activeVersion") {
				t.Fatalf("conflict did not return actual state: %d %s", conflict.Code, conflict.Body.String())
			}
		}
	}
}
