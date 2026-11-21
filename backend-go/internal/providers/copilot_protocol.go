package providers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// ActualServiceType is request-scoped evidence, never inferred from a model name.
func ActualServiceType(c fiber.Ctx, configured string) string {
	if configured == "copilot" {
		if value, ok := httpbridge.Value(c, "copilot_wire_service_type"); ok {
			if wire, ok := value.(string); ok {
				return wire
			}
		}
		return "responses"
	}
	return configured
}
