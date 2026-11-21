package middleware

import (
	"net"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

const contextEntryPolicy = "entryKeyPolicy"

func EntryPolicy(c fiber.Ctx) *config.EntryKey {
	value, _ := httpbridge.Value(c, contextEntryPolicy)
	policy, _ := value.(*config.EntryKey)
	return policy
}

func peerIP(c fiber.Ctx) string {
	raw := httpbridge.Request(c).RemoteAddr
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return host
	}
	return raw
}

func denyEntryPolicy(c fiber.Ctx) bool {
	httpbridge.JSON(c, 403, fiber.Map{"error": "entry_key_policy_denied"})
	httpbridge.Abort(c)
	return false
}

// AuthorizeProxyRoute is checked again at the real execution boundary, after
// manual routing, model mapping and failover; it never relies on UI filtering.
func AuthorizeProxyRoute(c fiber.Ctx, env *config.EnvConfig, model, channelUID string) bool {
	policy := EntryPolicy(c)
	if env != nil && env.ManagedProxyKeyLookup != nil {
		if fresh, ok := env.ManagedProxyKeyLookup(getAPIKey(c)); ok {
			policy = &fresh
		}
	}
	if policy == nil {
		return true
	}
	if !policy.ValidNow(time.Now()) || !policy.AllowsIP(peerIP(c)) || !policy.AllowsModel(model) || !policy.AllowsChannel(channelUID) {
		return denyEntryPolicy(c)
	}
	return true
}

// AuthorizeProxyModel applies model ACLs before any candidate selection.
func AuthorizeProxyModel(c fiber.Ctx, model string) bool {
	policy := EntryPolicy(c)
	return policy == nil || policy.AllowsModel(model) || denyEntryPolicy(c)
}

func setEntryPolicy(c fiber.Ctx, env *config.EnvConfig, providedKey string) (string, bool) {
	if env.ManagedProxyKeyLookup == nil {
		return "", true
	}
	policy, ok := env.ManagedProxyKeyLookup(providedKey)
	if !ok {
		return "", true
	}
	if !policy.ValidNow(time.Now()) || !policy.AllowsIP(peerIP(c)) {
		return "", denyEntryPolicy(c)
	}
	// Model-list/history operations do not carry a model/channel selection. A
	// scoped credential cannot use these unscoped metadata or memory endpoints.
	path := c.Path()
	if len(policy.Models) > 0 || len(policy.ChannelUIDs) > 0 {
		if strings.HasSuffix(path, "/v1/models") || (len(policy.ChannelUIDs) > 0 && strings.Contains(path, "/v1/models/")) || strings.Contains(path, "/v1/alpha/") {
			return "", denyEntryPolicy(c)
		}
	}
	if _, model, ok := strings.Cut(path, "/v1/models/"); ok && !policy.AllowsModel(model) {
		return "", denyEntryPolicy(c)
	}
	httpbridge.SetValue(c, contextEntryPolicy, &policy)
	httpbridge.SetValue(c, ContextKeyProxyKeyMask, "entry:"+policy.ID)
	return "principal:" + policy.PrincipalID, true
}
