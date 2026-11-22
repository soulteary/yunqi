package handlers

import (
	"errors"
	"fmt"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/presetstore"
	"net"
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

type priceErrorTransport struct{ detailed bool }

func (transport priceErrorTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport.detailed {
		return nil, fmt.Errorf("opaque-transport-private-value at %s", request.URL.String())
	}
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
}
func TestPriceReviewSourceErrorsNeverExposeCredentialsInAPIStateOrStatus(t *testing.T) {
	const source = "https://private-index-user:private-index-password@example.invalid/index.json?hint=private-index-query#private-index-fragment"
	for _, detailed := range []bool{false, true} {
		updater := presetstore.NewPresetUpdater(presetstore.NewPresetStore(nil), presetstore.UpdaterConfig{CacheDir: t.TempDir(), IndexURL: source, HTTPClient: &http.Client{Transport: priceErrorTransport{detailed: detailed}}})
		app := httpbridge.NewApp()
		app.Use(func(c fiber.Ctx) error {
			middleware.SetAdminPrincipal(c, middleware.AdminPrincipal{ID: "owner", Role: "admin"})
			return c.Next()
		})
		app.Post("/check", PriceReview(updater, "check"))
		app.Get("/status", presetstore.StatusHandler(updater))
		for _, path := range []string{"/check", "/status"} {
			method := http.MethodGet
			if path == "/check" {
				method = http.MethodPost
			}
			recorder := httptest.NewRecorder()
			httpbridge.Handler(app).ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
			text := recorder.Body.String()
			for _, secret := range []string{"private-index-user", "private-index-password", "private-index-query", "private-index-fragment", "opaque-transport-private-value"} {
				if strings.Contains(text, secret) {
					t.Fatalf("%s disclosed credential: %s", path, text)
				}
			}
			if path == "/check" && (recorder.Code != 500 || !strings.Contains(text, `"error":"source_check_failed"`) || !strings.Contains(text, `"persisted":false`)) {
				t.Fatalf("check lacked controlled failure/state: %d %s", recorder.Code, text)
			}
		}
		if updater.Status().LastError != "source_check_failed" {
			t.Fatal("status retained a free-form transport error")
		}
	}
}
