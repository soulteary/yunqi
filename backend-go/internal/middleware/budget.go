package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/entrybudget"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

type budgetConfigKey struct{}

func BudgetConfigManager(ctx context.Context) *config.ConfigManager {
	cm, _ := ctx.Value(budgetConfigKey{}).(*config.ConfigManager)
	return cm
}
func isBudgetedEndpoint(method, path string) bool {
	if method != http.MethodPost || !isDataPlaneRequest(method, path) {
		return false
	}
	return !strings.HasSuffix(path, "/v1/quote") && !strings.HasSuffix(path, "/count_tokens") && !strings.Contains(path, "/v1/alpha/")
}
func ProxyBudgetMiddleware(env *config.EnvConfig, cm *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		if !isBudgetedEndpoint(httpbridge.Request(c).Method, c.Path()) {
			return c.Next()
		}
		if cm == nil {
			return c.Next()
		}
		p, ok := cm.FindPrincipalBudget(GetPrincipalID(c))
		if !ok || p.Disabled {
			return c.Next()
		}
		if env.BudgetManager == nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "entry_budget_unavailable"})
			httpbridge.Abort(c)
			return nil
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return err
		}
		id := "req_" + hex.EncodeToString(nonce[:])
		// Reuse telemetry.DiagnosticRequestID once the diagnostics interface lands.
		session, err := env.BudgetManager.Start(httpbridge.Request(c).Context(), p, id)
		if err != nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "entry_budget_unavailable"})
			httpbridge.Abort(c)
			return nil
		}
		ctx := entrybudget.WithSession(httpbridge.Request(c).Context(), session)
		ctx = context.WithValue(ctx, budgetConfigKey{}, cm)
		httpbridge.SetRequest(c, httpbridge.Request(c).WithContext(ctx))
		httpbridge.SetValue(c, "entryBudgetRequestID", id)
		defer session.ParentDone()
		return c.Next()
	}
}
