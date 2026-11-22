package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

const ContextKeyAdminPrincipal = "adminPrincipal"

type AdminPrincipal struct {
	RevocationScope    string   `json:"revocationScope,omitempty"`
	Source             string   `json:"source,omitempty"`
	SessionExpiresAt   int64    `json:"sessionExpiresAt,omitempty"`
	SessionFingerprint string   `json:"sessionFingerprint,omitempty"`
	ID                 string   `json:"id"`
	Role               string   `json:"role"`
	ChannelUIDs        []string `json:"channelUids,omitempty"`
}

func SetAdminPrincipal(c fiber.Ctx, principal AdminPrincipal) {
	principal.ChannelUIDs = append([]string(nil), principal.ChannelUIDs...)
	httpbridge.SetValue(c, ContextKeyAdminPrincipal, principal)
}
func GetAdminPrincipal(c fiber.Ctx) AdminPrincipal {
	value, _ := httpbridge.Value(c, ContextKeyAdminPrincipal)
	principal, _ := value.(AdminPrincipal)
	principal.ChannelUIDs = append([]string(nil), principal.ChannelUIDs...)
	return principal
}
func GetAdminActorID(c fiber.Ctx) string { return GetAdminPrincipal(c).ID }

func CanAdminAction(c fiber.Ctx, action, resourceUID string) bool {
	p := GetAdminPrincipal(c)
	if p.ID == "" {
		return false
	}
	if action == "access.session.revoke" {
		return (p.Source == "identity_proxy" || p.Source == "oidc") && p.SessionFingerprint != ""
	}
	if action == "access.me" {
		return true
	}
	if action == "channels.list" {
		return p.Role == "viewer" || p.Role == "operator" || p.Role == "admin"
	}
	if resourceUID != "" && len(p.ChannelUIDs) > 0 {
		allowed := false
		for _, uid := range p.ChannelUIDs {
			if uid == resourceUID {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	} else if resourceUID == "" && len(p.ChannelUIDs) > 0 {
		return false
	}
	if p.Role == "admin" {
		return true
	}
	if action == "channels.read" && resourceUID != "" {
		return p.Role == "viewer" || p.Role == "operator"
	}
	return action == "channels.state" && resourceUID != "" && p.Role == "operator"
}

// AuthorizeAdminAction fails closed for unknown actions outside an admin role.
// Body log download/configuration and credential/config exports therefore need
// an explicit global admin (or a correctly scoped resource supplied by a caller).
func AuthorizeAdminAction(c fiber.Ctx, action, resourceUID string) bool {
	if CanAdminAction(c, action, resourceUID) {
		return true
	}
	httpbridge.JSON(c, http.StatusForbidden, fiber.Map{"error": "admin_permission_denied"})
	httpbridge.Abort(c)
	return false
}

func authenticateAdminPrincipal(c fiber.Ctx, env *config.EnvConfig, key string) {
	if env.IsStaticAdminAccessKey(key) {
		SetAdminPrincipal(c, AdminPrincipal{ID: "owner:" + config.EntryKeyHash(key), Role: "admin"})
		return
	}
	if env.ManagedAdminKeyLookup != nil {
		if credential, ok := env.ManagedAdminKeyLookup(key); ok && credential.ValidNow(time.Now()) {
			SetAdminPrincipal(c, AdminPrincipal{ID: "admin:" + credential.ActorID, Role: credential.Role, ChannelUIDs: credential.ChannelUIDs})
		}
	}
}

// Existing management APIs may reveal raw provider credentials. Limited roles
// use explicit safe APIs; new legacy endpoints do not gain read access by default.
func authorizeManagementRoute(c fiber.Ctx) bool {
	path, method := c.Path(), httpbridge.Request(c).Method
	switch {
	case method == http.MethodPost && path == "/api/access/session/revoke":
		return AuthorizeAdminAction(c, "access.session.revoke", "")
	case method == http.MethodGet && path == "/api/access/me":
		return AuthorizeAdminAction(c, "access.me", "")
	case method == http.MethodGet && path == "/api/access/channels":
		return AuthorizeAdminAction(c, "channels.list", "")
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 && parts[0] == "api" && parts[1] == "channels" && parts[2] != "" && parts[3] == "model-import" {
		if len(parts) == 4 && method == http.MethodGet {
			return AuthorizeAdminAction(c, "channels.read", parts[2])
		}
		if len(parts) == 5 && ((parts[4] == "policy" && method == http.MethodPut) || ((parts[4] == "preview" || parts[4] == "approve") && method == http.MethodPost)) {
			return AuthorizeAdminAction(c, "channels.model-import", parts[2])
		}
	}
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "access" && parts[2] == "channels" && parts[3] != "" && parts[4] == "state" && method == http.MethodPut {
		return AuthorizeAdminAction(c, "channels.state", parts[3])
	}
	return AuthorizeAdminAction(c, "admin.legacy", "")
}
