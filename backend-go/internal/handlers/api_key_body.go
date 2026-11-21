package handlers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// APIKeyFromBody adapts the existing key mutation handlers without exposing the
// key in the URL. Legacy path-based routes remain available for older clients.
func APIKeyFromBody(handler fiber.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req struct {
			APIKey string `json:"apiKey"`
		}
		if err := httpbridge.BindJSON(c, &req); err != nil || req.APIKey == "" {
			httpbridge.JSON(c, fiber.StatusBadRequest, fiber.Map{"error": "A non-empty apiKey is required"})
			return nil
		}
		httpbridge.SetParams(c, httpbridge.Params{{Key: "id", Value: httpbridge.Param(c, "id")}, {Key: "apiKey", Value: req.APIKey}})
		return handler(c)
	}
}
