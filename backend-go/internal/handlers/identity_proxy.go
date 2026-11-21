package handlers

import (
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
		count := len(cfg.RevokedSessionHashes)
		cfg.RevokedSessionHashes = nil
		httpbridge.JSON(c, 200, fiber.Map{"config": cfg, "secretConfigured": len(env.IdentityProxySecret) >= 32, "revokedSessionCount": count})
		return nil
	})
	router.Put("/identity-proxy", func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "identity.configure", "") {
			return nil
		}
		var cfg config.IdentityProxyConfig
		if httpbridge.BindJSON(c, &cfg) != nil || cfg.Enabled && len(env.IdentityProxySecret) < 32 {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_identity_proxy_config"})
			return nil
		}
		if err := cm.SetIdentityProxyConfig(cfg); err != nil {
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
		}
		if httpbridge.BindJSON(c, &req) != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_session_fingerprint"})
			return nil
		}
		if err := cm.RevokeIdentitySession(req.SessionFingerprint); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "session_revocation_failed"})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"revoked": true})
		return nil
	})
	router.Post("/access/session/revoke", func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "access.session.revoke", "") {
			return nil
		}
		if err := cm.RevokeIdentitySession(middleware.GetAdminPrincipal(c).SessionFingerprint); err != nil {
			httpbridge.JSON(c, 503, fiber.Map{"error": "session_revocation_failed"})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"revoked": true})
		return nil
	})
}
