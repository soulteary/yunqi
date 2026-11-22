package handlers

import (
	"encoding/json"
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/presetstore"
	"io"
	"net/http"
)

func PriceReview(updater *presetstore.PresetUpdater, operation string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "pricing.review", "") {
			return nil
		}
		httpbridge.Header(c, "Cache-Control", "no-store")
		if updater == nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "preset updater is unavailable"})
			return nil
		}
		var err error
		switch operation {
		case "state":
		case "check":
			err = updater.CheckOnce(httpbridge.Request(c).Context())
		case "approve", "rollback":
			data, readErr := io.ReadAll(io.LimitReader(httpbridge.Request(c).Body, 4097))
			if readErr != nil || len(data) > 4096 {
				httpbridge.JSON(c, 400, fiber.Map{"error": "invalid price review request"})
				return nil
			}
			var input struct {
				ReviewID       string `json:"reviewId"`
				BundleHash     string `json:"bundleHash"`
				AllowAnomalies bool   `json:"allowAnomalies"`
			}
			if json.Unmarshal(data, &input) != nil {
				httpbridge.JSON(c, 400, fiber.Map{"error": "invalid price review JSON"})
				return nil
			}
			actor := middleware.GetAdminActorID(c)
			if operation == "approve" {
				if input.ReviewID == "" {
					httpbridge.JSON(c, 400, fiber.Map{"error": "reviewId is required"})
					return nil
				}
				err = updater.ApprovePriceReview(actor, input.ReviewID, input.AllowAnomalies)
			} else {
				if len(input.BundleHash) != 64 {
					httpbridge.JSON(c, 400, fiber.Map{"error": "bundleHash is required"})
					return nil
				}
				err = updater.RollbackPriceVersion(actor, input.BundleHash)
			}
		default:
			httpbridge.JSON(c, 400, fiber.Map{"error": "unsupported price review operation"})
			return nil
		}
		state := updater.PriceReviewState()
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, presetstore.ErrPriceReviewConflict) {
				status = 409
			} else if errors.Is(err, presetstore.ErrPriceRollbackUnavailable) {
				status = 404
			} else if errors.Is(err, presetstore.ErrPriceAnomalyAck) {
				status = 400
			}
			httpbridge.JSON(c, status, fiber.Map{"error": err.Error(), "persisted": false, "state": state})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"persisted": operation == "approve" || operation == "rollback", "state": state})
		return nil
	}
}
