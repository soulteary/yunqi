package httpbridge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/redaction"
)

func TestErrorAndDiagnosticBoundariesRedactWithoutChangingCredentialAPIs(t *testing.T) {
	const secret = "unknown!vendor-key=opaque/2026"
	redaction.Configure([]string{secret})
	defer redaction.Configure(nil)
	for _, test := range []struct {
		name, path           string
		status               int
		native, text, masked bool
	}{
		{name: "JSON error", path: "/v1/chat/completions", status: 502, masked: true},
		{name: "native Fiber error", path: "/api/test", status: 400, native: true, masked: true},
		{name: "diagnostic download", path: "/api/request-logs", status: 200, masked: true},
		{name: "diagnostic text", path: "/api/network/diagnose", status: 200, text: true, masked: true},
		{name: "token exchange remains usable", path: "/api/accounts/credentials", status: 200},
		{name: "successful model content unchanged", path: "/v1/chat/completions", status: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := NewApp()
			app.Get(test.path, func(c fiber.Ctx) error {
				if test.native {
					return c.Status(test.status).JSON(fiber.Map{"message": secret})
				}
				if test.text {
					Data(c, test.status, "text/plain", []byte(secret))
					return nil
				}
				JSON(c, test.status, fiber.Map{"message": secret})
				return nil
			})
			response := httptest.NewRecorder()
			Handler(app).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != test.status {
				t.Fatalf("status=%d", response.Code)
			}
			if leaked := strings.Contains(response.Body.String(), secret); leaked == test.masked {
				t.Fatalf("masked=%v response=%s", test.masked, response.Body.String())
			}
		})
	}
}
