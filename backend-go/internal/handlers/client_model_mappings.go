package handlers

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func GetClientModelMappings(manager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		httpbridge.Header(c, "Cache-Control", "no-store")
		httpbridge.JSON(c, http.StatusOK, fiber.Map{"mappings": manager.GetClientModelMappings(), "targets": manager.ClientMappingTargets()})
		return nil
	}
}
func PutClientModelMappings(manager *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request struct {
			Mappings config.ClientModelMappings `json:"mappings"`
		}
		if err := httpbridge.BindJSON(c, &request); err != nil || request.Mappings == nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": "mappings must be an object"})
			return nil
		}
		if err := manager.SetClientModelMappingsAudited(config.ManagementAuditFromContext(httpbridge.Request(c).Context()), request.Mappings); err != nil {
			httpbridge.JSON(c, http.StatusBadRequest, fiber.Map{"error": err.Error()})
			return nil
		}
		return GetClientModelMappings(manager)(c)
	}
}
