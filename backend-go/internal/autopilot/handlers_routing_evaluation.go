package autopilot

import (
	"database/sql"
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/routingeval"
	"io"
)

func RegisterRoutingEvaluationRoutes(group fiber.Router, ab *ABTestStore) {
	var store *routingeval.Store
	if ab != nil {
		store, _ = routingeval.NewStore(ab.db)
	}
	guard := func(c fiber.Ctx) bool {
		p := middleware.GetAdminPrincipal(c)
		if p.Role != "admin" || len(p.ChannelUIDs) > 0 {
			httpbridge.JSON(c, 403, fiber.Map{"error": "global_admin_required"})
			return false
		}
		if store == nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "evaluation_persistence_unavailable"})
			return false
		}
		return true
	}
	group.Post("/autopilot/evaluations", func(c fiber.Ctx) error {
		if !guard(c) {
			return nil
		}
		raw, err := io.ReadAll(io.LimitReader(httpbridge.Request(c).Body, (4<<20)+1))
		if err != nil || len(raw) > 4<<20 {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_evaluation_dataset"})
			return nil
		}
		d, err := routingeval.Decode(raw)
		if err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_evaluation_dataset"})
			return nil
		}
		if _, err = routingeval.Evaluate(d); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_evaluation_dataset"})
			return nil
		}
		saved, err := store.Save(httpbridge.Request(c).Context(), d)
		if err != nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "evaluation_persistence_unavailable"})
			return nil
		}
		httpbridge.JSON(c, 200, saved)
		return nil
	})
	group.Get("/autopilot/evaluations", func(c fiber.Ctx) error {
		if !guard(c) {
			return nil
		}
		runs, err := store.List(httpbridge.Request(c).Context())
		if err != nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "evaluation_persistence_unavailable"})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"runs": runs, "retentionLimit": routingeval.RetentionLimit})
		return nil
	})
	group.Get("/autopilot/evaluations/:id/dataset", func(c fiber.Ctx) error {
		if !guard(c) {
			return nil
		}
		dataset, err := store.Dataset(httpbridge.Request(c).Context(), httpbridge.Param(c, "id"))
		if errors.Is(err, sql.ErrNoRows) {
			httpbridge.JSON(c, 404, fiber.Map{"error": "evaluation_not_found"})
			return nil
		}
		if err != nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "evaluation_persistence_unavailable"})
			return nil
		}
		httpbridge.JSON(c, 200, dataset)
		return nil
	})
}
