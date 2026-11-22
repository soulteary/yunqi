package handlers

import (
	"context"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/nativeoidc"
	"net/http"
	"time"
)

func RegisterNativeOIDCRoutes(r fiber.Router, env *config.EnvConfig) {
	if env.NativeOIDCIssuer == "" {
		return
	}
	manager, err := nativeoidc.New(env)
	if err == nil {
		env.NativeIdentity = manager
	}
	fail := func(c fiber.Ctx, status int) error {
		httpbridge.Writer(c).Header().Set("Cache-Control", "no-store")
		httpbridge.JSON(c, status, fiber.Map{"error": "oidc_login_failed"})
		return nil
	}
	r.Get("/auth/oidc/login", func(c fiber.Ctx) error {
		if !env.EnableWebUI {
			return fail(c, 404)
		}
		if manager == nil {
			return fail(c, 503)
		}
		ctx, cancel := context.WithTimeout(httpbridge.Request(c).Context(), 15*time.Second)
		defer cancel()
		destination, browser, err := manager.Begin(ctx)
		if err != nil {
			return fail(c, 503)
		}
		headers := httpbridge.Writer(c).Header()
		headers.Set("Cache-Control", "no-store")
		headers.Set("Referrer-Policy", "no-referrer")
		http.SetCookie(httpbridge.Writer(c), &http.Cookie{Name: nativeoidc.FlowCookieName, Value: browser, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 300})
		headers.Set("Location", destination)
		httpbridge.Status(c, 302)
		return nil
	})
	r.Get("/auth/oidc/callback", func(c fiber.Ctx) error {
		if !env.EnableWebUI {
			return fail(c, 404)
		}
		if manager == nil {
			return fail(c, 503)
		}
		q := httpbridge.Request(c).URL.Query()
		if len(q["state"]) != 1 || len(q["code"]) != 1 || q.Get("error") != "" {
			return fail(c, 401)
		}
		cookie, err := httpbridge.Request(c).Cookie(nativeoidc.FlowCookieName)
		if err != nil {
			return fail(c, 401)
		}
		http.SetCookie(httpbridge.Writer(c), &http.Cookie{Name: nativeoidc.FlowCookieName, Value: "", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		ctx, cancel := context.WithTimeout(httpbridge.Request(c).Context(), 15*time.Second)
		defer cancel()
		token, err := manager.Complete(ctx, q.Get("state"), q.Get("code"), cookie.Value)
		if err != nil {
			if nativeoidc.IsUnavailable(err) {
				return fail(c, 503)
			}
			return fail(c, 401)
		}
		session, err := manager.Lookup(ctx, token)
		if err != nil {
			return fail(c, 503)
		}
		http.SetCookie(httpbridge.Writer(c), &http.Cookie{Name: nativeoidc.CookieName, Value: token, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, Expires: time.Unix(session.ExpiresAt, 0)})
		headers := httpbridge.Writer(c).Header()
		headers.Set("Cache-Control", "no-store")
		headers.Set("Referrer-Policy", "no-referrer")
		headers.Set("Location", "/access")
		httpbridge.Status(c, 302)
		return nil
	})
}
func ClearNativeIdentityCookie(c fiber.Ctx) {
	if middleware.GetAdminPrincipal(c).Source == "oidc" {
		http.SetCookie(httpbridge.Writer(c), &http.Cookie{Name: nativeoidc.CookieName, Path: "/", Value: "", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	}
}
