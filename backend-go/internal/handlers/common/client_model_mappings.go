package common

import (
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func ApplyClientModelMapping(c fiber.Ctx, manager *config.ConfigManager, body []byte, kind string) ([]byte, error) {
	model := gjson.GetBytes(body, "model").String()
	target, reserved := manager.ResolveClientModelAlias(model, kind)
	if !reserved {
		return body, nil
	}
	if target == "" || httpbridge.Param(c, "routePrefix") != "" {
		httpbridge.JSON(c, 400, fiber.Map{"error": fiber.Map{"type": "invalid_request_error", "message": "Client model mapping is unavailable for this protocol or route"}})
		return nil, fmt.Errorf("unavailable client model mapping")
	}
	mapped, err := sjson.SetBytes(body, "model", target)
	if err != nil || !gjson.ValidBytes(mapped) {
		httpbridge.JSON(c, 400, fiber.Map{"error": fiber.Map{"type": "invalid_request_error", "message": "Invalid JSON request"}})
		return nil, fmt.Errorf("invalid mapped request JSON")
	}
	return mapped, nil
}
