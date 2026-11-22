package handlers

import (
	_ "embed"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"strings"
)

//go:embed access_portal.html
var accessPortalHTML string

// AccessPortal is the small, safe management entry for limited administrators.
// All privileged data and writes still pass the same authenticated API gates.
func AccessPortal(env *config.EnvConfig) fiber.Handler {
	return func(c fiber.Ctx) error {
		if !env.EnableWebUI {
			httpbridge.Status(c, 404)
			return nil
		}
		nonce, err := randomEntryToken(18)
		if err != nil {
			return err
		}
		header := httpbridge.Writer(c).Header()
		header.Set("Cache-Control", "no-store")
		header.Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'nonce-"+nonce+"'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		enabled := "false"
		if env.IdentityProxyConfigLookup != nil && len(env.IdentityProxySecret) >= 32 {
			cfg := env.IdentityProxyConfigLookup()
			if (cfg.EffectiveRevocationMode() == "local" || cfg.EffectiveRevocationMode() == "shared" && env.IdentityProxyRevocations != nil) && cfg.Enabled && cfg.Issuer != "" && len(cfg.TrustedCIDRs) > 0 && len(cfg.GroupMappings) > 0 && len(cfg.AllowedOrigins) > 0 {
				enabled = "true"
			}
		}
		html := strings.ReplaceAll(accessPortalHTML, "__NONCE__", nonce)
		html = strings.ReplaceAll(html, "__IDENTITY_PROXY_ENABLED__", enabled)
		httpbridge.Data(c, 200, "text/html; charset=utf-8", []byte(html))
		return nil
	}
}
