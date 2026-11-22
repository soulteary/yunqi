package handlers

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
)

func RegisterIdentityProxyRoutes(router fiber.Router, env *config.EnvConfig, cm *config.ConfigManager) {
	router.Get("/identity-proxy", func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "identity.configure", "") {
			return nil
		}
		cfg := cm.IdentityProxySnapshot()
		cfg.RevocationMode = cfg.EffectiveRevocationMode()
		count := len(cfg.RevokedSessions)
		cfg.RevokedSessions = nil
		httpbridge.JSON(c, 200, fiber.Map{"config": cfg, "secretConfigured": len(env.IdentityProxySecret) >= 32, "revokedSessionCount": count, "revokedSessionCountScope": "local_audit", "sharedRevocationConfigured": env.IdentityProxyRevocations != nil})
		return nil
	})
	router.Put("/identity-proxy", func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if !middleware.AuthorizeAdminAction(c, "identity.configure", "") {
			return nil
		}
		var cfg config.IdentityProxyConfig
		if httpbridge.BindJSON(c, &cfg) != nil || cfg.Enabled && len(env.IdentityProxySecret) < 32 {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_identity_proxy_config"})
			return nil
		}
		effectiveMode := cfg.RevocationMode
		if effectiveMode == "" {
			effectiveMode = cm.IdentityProxySnapshot().EffectiveRevocationMode()
		}
		if cfg.Enabled && effectiveMode == "shared" {
			if env.IdentityProxyRevocations == nil {
				httpbridge.JSON(c, 400, fiber.Map{"error": "shared_revocation_backend_required"})
				return nil
			}
			call, cancel := context.WithTimeout(httpbridge.Request(c).Context(), 2*time.Second)
			previous := cm.IdentityProxySnapshot()
			expected := cfg.RevocationEpoch
			if expected == "" {
				expected = previous.RevocationEpoch
			}
			initialize := cfg.RevocationMode == "shared" && previous.EffectiveRevocationMode() == "local" && expected == ""
			epoch, err := env.IdentityProxyRevocations.Prepare(call, expected, initialize)
			if err == nil {
				err = env.IdentityProxyRevocations.Sync(call, previous.RevokedSessions)
			}
			cfg.RevocationEpoch = epoch
			if err == nil {
				_, err = env.IdentityProxyRevocations.IsRevoked(call, strings.Repeat("0", 64))
			}
			cancel()
			if err != nil {
				httpbridge.JSON(c, 503, fiber.Map{"error": "identity_proxy_revocation_unavailable"})
				return nil
			}
		}
		if err := cm.SetIdentityProxyConfigAudited(managementAuditContext, cfg); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_identity_proxy_config"})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"updated": true})
		return nil
	})
	router.Post("/identity-proxy/sessions/revoke", func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "identity.sessions.revoke", "") {
			return nil
		}
		var req struct {
			SessionFingerprint string `json:"sessionFingerprint"`
			ExpiresAt          int64  `json:"expiresAt,omitempty"`
		}
		if httpbridge.BindJSON(c, &req) != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_session_fingerprint"})
			return nil
		}
		decoded, decodeErr := hex.DecodeString(strings.ToLower(req.SessionFingerprint))
		if decodeErr != nil || len(decoded) != 32 || req.ExpiresAt < 0 {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_session_fingerprint"})
			return nil
		}
		if req.ExpiresAt > 0 && req.ExpiresAt <= time.Now().Unix() {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_session_expiry"})
			return nil
		}
		scope, err := revokeIdentitySession(c, env, cm, req.SessionFingerprint, req.ExpiresAt)
		if err != nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "session_revocation_failed"})
			return nil
		}
		ClearNativeIdentityCookie(c, req.SessionFingerprint)
		httpbridge.JSON(c, 200, fiber.Map{"revoked": true, "revocationScope": scope})
		return nil
	})
	router.Post("/access/session/revoke", func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "access.session.revoke", "") {
			return nil
		}
		scope, err := revokeIdentitySession(c, env, cm, middleware.GetAdminPrincipal(c).SessionFingerprint, middleware.GetAdminPrincipal(c).SessionExpiresAt)
		if err != nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "session_revocation_failed"})
			return nil
		}
		ClearNativeIdentityCookie(c, middleware.GetAdminPrincipal(c).SessionFingerprint)
		httpbridge.JSON(c, 200, fiber.Map{"revoked": true, "revocationScope": scope})
		return nil
	})
}

func revokeIdentitySession(c fiber.Ctx, env *config.EnvConfig, cm *config.ConfigManager, fingerprint string, expires int64) (string, error) {
	managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

	cfg := cm.IdentityProxySnapshot()
	scope := cfg.EffectiveRevocationMode()
	if middleware.GetAdminPrincipal(c).RevocationScope == "shared" {
		scope = "shared"
	}
	if scope == "shared" {
		if env.IdentityProxyRevocations == nil || cfg.RevocationEpoch == "" {
			return scope, errors.New("shared identity revocation unavailable")
		}
		call, cancel := context.WithTimeout(httpbridge.Request(c).Context(), 2*time.Second)
		defer cancel()
		if _, err := env.IdentityProxyRevocations.Prepare(call, cfg.RevocationEpoch, false); err != nil {
			return scope, err
		}
		if err := env.IdentityProxyRevocations.Sync(call, cfg.RevokedSessions); err != nil {
			return scope, err
		}
		if err := env.IdentityProxyRevocations.Revoke(call, fingerprint, expires); err != nil {
			return scope, err
		}
	} else if scope != "local" {
		return scope, errors.New("invalid revocation scope")
	}
	return scope, cm.RevokeIdentitySessionUntilAudited(managementAuditContext, fingerprint, expires)
}
