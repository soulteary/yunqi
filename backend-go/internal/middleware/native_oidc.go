package middleware

import (
	"context"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/nativeoidc"
	"net/http"
	"strings"
	"time"
)

func authenticateNativeIdentity(c fiber.Ctx, env *config.EnvConfig) bool {
	cookie, err := httpbridge.Request(c).Cookie(nativeoidc.CookieName)
	if err != nil {
		return false
	}
	if env.NativeIdentity == nil {
		if env.NativeOIDCIssuer != "" {
			return rejectSharedIdentity(c)
		}
		return false
	}
	origin := httpbridge.GetHeader(c, "Origin")
	if origin != "" && origin != nativeOrigin(env.NativeOIDCRedirectURL) {
		return false
	}
	method := httpbridge.Request(c).Method
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		if origin != nativeOrigin(env.NativeOIDCRedirectURL) || httpbridge.GetHeader(c, IdentityManagementHeader) != "1" {
			return false
		}
	}
	call, cancel := context.WithTimeout(httpbridge.Request(c).Context(), 3*time.Second)
	defer cancel()
	session, err := env.NativeIdentity.Lookup(call, cookie.Value)
	if err != nil {
		if nativeoidc.IsUnavailable(err) {
			return rejectSharedIdentity(c)
		}
		return false
	}
	cfg := env.IdentityProxyConfigLookup()
	present := make(map[string]bool)
	for _, g := range session.Groups {
		present[g] = true
	}
	role, channels, ok := identityGroupPermissions(cfg.GroupMappings, present)
	if !ok {
		return false
	}
	SetAdminPrincipal(c, AdminPrincipal{ID: "sso:" + session.SubjectID, Role: role, ChannelUIDs: channels, Source: "oidc", RevocationScope: cfg.EffectiveRevocationMode(), SessionFingerprint: session.Fingerprint, SessionExpiresAt: session.ExpiresAt})
	return true
}
func nativeOrigin(redirect string) string {
	before, _, _ := strings.Cut(redirect, "/auth/oidc/callback")
	origin, _ := config.NormalizeIdentityOrigin(before)
	return origin
}
