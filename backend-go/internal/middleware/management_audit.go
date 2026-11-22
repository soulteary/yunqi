package middleware

import (
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"log"
	"net/http"
	"strings"
)

func isManagementMutation(c fiber.Ctx) bool {
	method := httpbridge.Request(c).Method
	return (method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete) && (strings.HasPrefix(c.Path(), "/api") || strings.HasPrefix(c.Path(), "/admin"))
}

// ManagementAuditMiddleware creates operation identity before authentication;
// WebAuth persists the actor-bound intent BEFORE permitting a handler to run.
func ManagementAuditMiddleware(cm *config.ConfigManager) fiber.Handler {
	return func(c fiber.Ctx) (returned error) {
		if cm == nil || !isManagementMutation(c) {
			return c.Next()
		}
		action := httpbridge.Request(c).Method + " management"
		parts := strings.Split(strings.Trim(c.Path(), "/"), "/")
		if len(parts) > 1 {
			switch parts[1] {
			case "messages", "chat", "responses", "gemini", "images", "vectors", "accounts", "subscriptions", "smart-routing", "task-templates", "channel-discovery", "channel-discovery-fast", "channels", "access", "identity-proxy", "budgets", "presets", "logging", "racing", "settings", "console", "conversations", "autopilot", "config":
				action = httpbridge.Request(c).Method + " " + parts[1]
			}
		}
		audit, err := config.NewManagementAuditContext("unauthenticated", action, "path:"+config.EntryKeyHash(c.Path()))
		if err != nil {
			return rejectManagementAudit(c)
		}
		request := httpbridge.Request(c)
		httpbridge.SetRequest(c, request.WithContext(config.WithManagementAudit(request.Context(), audit)))
		httpbridge.Writer(c).Header().Set("X-Yunqi-Audit-Operation", audit.OperationID)
		defer func() {
			panicValue := recover()
			audit := config.ManagementAuditFromContext(httpbridge.Request(c).Context())
			if GetAdminPrincipal(c).ID == "" && audit.ActorID == "unauthenticated" {
				if panicValue != nil {
					panic(panicValue)
				}
				return
			}
			status := httpbridge.Writer(c).Status()
			outcome := "completed"
			switch {
			case panicValue != nil || returned != nil:
				status = 500
				var fiberError *fiber.Error
				if errors.As(returned, &fiberError) {
					status = fiberError.Code
				}
				outcome = "failed"
			case status == http.StatusAccepted:
				outcome = "accepted"
			case status == 401 || status == 403:
				outcome = "rejected"
			case status >= 400:
				outcome = "failed"
			}
			if err := cm.RecordManagementRequest(audit, outcome, status); err != nil && panicValue == nil {
				if !httpbridge.Writer(c).Written() {
					returned = rejectManagementAudit(c)
				} else {
					log.Print("[Management-Audit] outcome persistence failed; inspect existing audit records; no completion is implied")
				}
			}
			if panicValue != nil {
				panic(panicValue)
			}
		}()
		return c.Next()
	}
}
func beginAuthenticatedManagementAudit(c fiber.Ctx, cm *config.ConfigManager) bool {
	audit := config.ManagementAuditFromContext(httpbridge.Request(c).Context())
	if cm == nil || audit.OperationID == "" {
		return true
	}
	audit.ActorID = GetAdminActorID(c)
	if audit.ActorID == "" {
		audit.ActorID = "unresolved_admin_identity"
	}
	request := httpbridge.Request(c)
	httpbridge.SetRequest(c, request.WithContext(config.WithManagementAudit(request.Context(), audit)))
	if cm.RecordManagementRequest(audit, "intent", 0) != nil {
		_ = rejectManagementAudit(c)
		return false
	}
	return true
}
func rejectManagementAudit(c fiber.Ctx) error {
	httpbridge.JSON(c, 503, fiber.Map{"error": "management_audit_unavailable"})
	httpbridge.Abort(c)
	return nil
}
