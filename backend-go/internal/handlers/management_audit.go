package handlers

import (
	"encoding/base64"
	"encoding/json"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/presetstore"
	"strconv"
)

type auditCursor struct {
	Before   uint64 `json:"before"`
	Snapshot uint64 `json:"snapshot"`
	Filter   string `json:"filter"`
	Anchor   string `json:"anchor"`
}

func RegisterManagementAuditRoutes(router fiber.Router, cm *config.ConfigManager, prices *presetstore.PresetUpdater) {
	router.Get("/audit", func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "audit.read", "") {
			return nil
		}
		httpbridge.Header(c, "Cache-Control", "no-store")
		limit, err := strconv.Atoi(httpbridge.Query(c, "limit", "100"))
		if err != nil || limit < 1 || limit > 200 {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_audit_limit"})
			return nil
		}
		filters := map[string]string{}
		for _, key := range []string{"actorId", "action", "outcome", "operationId"} {
			value := httpbridge.Query(c, key)
			if len(value) > 256 {
				httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_audit_filter"})
				return nil
			}
			filters[key] = value
		}
		encoded, _ := json.Marshal(struct {
			Filters map[string]string
			Limit   int
		}{filters, limit})
		digest := config.EntryKeyHash(string(encoded))
		state := cm.ManagementAuditSnapshot()
		cursor := auditCursor{Before: state.NextSequence + 1, Snapshot: state.NextSequence, Filter: digest, Anchor: managementAuditAnchor(state, state.NextSequence)}
		raw := httpbridge.Query(c, "cursor")
		if raw != "" {
			if len(raw) > 512 {
				httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_audit_cursor"})
				return nil
			}
			decoded, err := base64.RawURLEncoding.DecodeString(raw)
			if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Filter != digest || cursor.Before == 0 || cursor.Before > cursor.Snapshot+1 || cursor.Snapshot > state.NextSequence {
				httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_audit_cursor"})
				return nil
			}
			if len(state.Events) > 0 && cursor.Before <= state.Events[0].Sequence {
				httpbridge.JSON(c, 409, fiber.Map{"error": "audit_history_expired"})
				return nil
			}
			if cursor.Anchor == "" || cursor.Anchor != managementAuditAnchor(state, cursor.Snapshot) {
				httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_audit_cursor"})
				return nil
			}
		}
		entries := []config.ManagementAuditEvent{}
		for i := len(state.Events) - 1; i >= 0; i-- {
			event := state.Events[i]
			if event.Sequence >= cursor.Before || event.Sequence > cursor.Snapshot {
				continue
			}
			if filters["actorId"] != "" && event.ActorID != filters["actorId"] || filters["action"] != "" && event.Action != filters["action"] || filters["outcome"] != "" && event.Outcome != filters["outcome"] || filters["operationId"] != "" && event.OperationID != filters["operationId"] {
				continue
			}
			entries = append(entries, event)
			if len(entries) > limit {
				break
			}
		}
		next := ""
		if len(entries) > limit {
			entries = entries[:limit]
			cursor.Before = entries[len(entries)-1].Sequence
			data, _ := json.Marshal(cursor)
			next = base64.RawURLEncoding.EncodeToString(data)
		}
		operations := map[string]bool{}
		for _, entry := range entries {
			if entry.OperationID != "" {
				operations[entry.OperationID] = true
			}
		}
		body := []config.BodyLogAuditEvent{}
		for _, entry := range cm.GetBodyLoggingState().Audit {
			if entry.OperationID != "" && operations[entry.OperationID] {
				body = append(body, entry)
			}
		}
		price := []presetstore.PriceAudit{}
		if prices != nil {
			for _, entry := range prices.PriceReviewState().Audit {
				if entry.OperationID != "" && operations[entry.OperationID] {
					price = append(price, entry)
				}
			}
		}
		first := uint64(0)
		if len(state.Events) > 0 {
			first = state.Events[0].Sequence
		}
		httpbridge.JSON(c, 200, fiber.Map{"entries": entries, "nextCursor": next, "snapshotSequence": cursor.Snapshot, "firstRetainedSequence": first, "maxEntries": config.ManagementAuditMaxEntries, "maxBytes": config.ManagementAuditMaxBytes, "related": fiber.Map{"bodyLogging": body, "prices": price}})
		return nil
	})
}

func managementAuditAnchor(state config.ManagementAuditState, sequence uint64) string {
	for i := len(state.Events) - 1; i >= 0; i-- {
		if state.Events[i].Sequence == sequence {
			data, _ := json.Marshal(state.Events[i])
			return config.EntryKeyHash(string(data))
		}
	}
	return ""
}
