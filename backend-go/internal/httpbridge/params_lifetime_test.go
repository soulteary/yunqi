package httpbridge

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestRouteParameterRemainsStableAcrossRequests(t *testing.T) {
	app := NewApp()
	var retained string
	app.Get("/items/:value", func(c fiber.Ctx) error {
		if retained == "" {
			retained = Param(c, "value")
		}
		return nil
	})
	handler := Handler(app)
	for _, path := range []string{"/items/first-secret", "/items/other-public", "/items/third-public"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if retained != "first-secret" {
			t.Fatalf("retained route parameter changed after %s: %q", path, retained)
		}
	}
}
