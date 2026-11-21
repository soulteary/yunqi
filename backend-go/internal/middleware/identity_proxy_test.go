package middleware

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"net/http/httptest"
	"testing"
)

func TestDataPlaneStripsIdentityAssertionsBeforeProxyExecution(t *testing.T) {
	env := &config.EnvConfig{ProxyAccessKey: "proxy"}
	app := httpbridge.NewApp()
	app.Use(WebAuthMiddleware(env, nil))
	app.Use(ProxyAdmissionMiddleware(env))
	app.Post("/v1/messages", func(c fiber.Ctx) error {
		if httpbridge.GetHeader(c, IdentitySecretHeader) != "" || len(c.Request().Header.Peek(IdentitySubjectHeader)) > 0 || httpbridge.GetHeader(c, IdentityManagementHeader) != "" {
			t.Fatal("reserved identity header escaped authentication boundary")
		}
		httpbridge.Text(c, 200, "ok")
		return nil
	})
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("x-api-key", "proxy")
	req.Header.Set(IdentitySecretHeader, "secret")
	req.Header.Set(IdentitySubjectHeader, "person")
	req.Header.Set(IdentityManagementHeader, "1")
	w := httptest.NewRecorder()
	httpbridge.Handler(app).ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("normal proxy key changed: %d", w.Code)
	}
}
