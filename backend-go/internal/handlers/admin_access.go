package handlers

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
)

type adminCredentialView struct {
	ID          string     `json:"id"`
	ActorID     string     `json:"actorId"`
	Role        string     `json:"role"`
	ChannelUIDs []string   `json:"channelUids,omitempty"`
	Disabled    bool       `json:"disabled"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
}

func publicAdminCredential(key config.AdminCredential) adminCredentialView {
	return adminCredentialView{key.ID, key.ActorID, key.Role, key.ChannelUIDs, key.Disabled, key.ExpiresAt}
}
func adminCredentialByID(cm *config.ConfigManager, id string) (config.AdminCredential, error) {
	for _, key := range cm.AdminCredentialsSnapshot() {
		if key.ID == id {
			return key, nil
		}
	}
	return config.AdminCredential{}, errors.New("credential not found")
}

func RegisterAdminAccessRoutes(router fiber.Router, env *config.EnvConfig, cm *config.ConfigManager) {
	router.Get("/access/me", func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "access.me", "") {
			return nil
		}
		httpbridge.JSON(c, 200, middleware.GetAdminPrincipal(c))
		return nil
	})
	router.Get("/access/channels", func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "channels.list", "") {
			return nil
		}
		cfg := cm.GetConfig()
		views := make([]fiber.Map, 0)
		for _, group := range []struct {
			kind     string
			channels []config.UpstreamConfig
		}{{"messages", cfg.Upstream}, {"chat", cfg.ChatUpstream}, {"responses", cfg.ResponsesUpstream}, {"gemini", cfg.GeminiUpstream}, {"images", cfg.ImagesUpstream}, {"vectors", cfg.VectorsUpstream}} {
			for _, up := range group.channels {
				if middleware.CanAdminAction(c, "channels.read", up.ChannelUID) {
					views = append(views, fiber.Map{"channelUid": up.ChannelUID, "kind": group.kind, "name": up.Name, "status": config.GetChannelEffectiveState(&up), "adminState": config.GetChannelAdminState(&up)})
				}
			}
		}
		httpbridge.JSON(c, 200, fiber.Map{"channels": views})
		return nil
	})
	router.Put("/access/channels/:uid/state", func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		uid := httpbridge.Param(c, "uid")
		if !middleware.AuthorizeAdminAction(c, "channels.state", uid) {
			return nil
		}
		var req struct {
			Status string `json:"status"`
		}
		if httpbridge.BindJSON(c, &req) != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_channel_state"})
			return nil
		}
		if err := cm.SetChannelStatusByUIDAudited(managementAuditContext, uid, req.Status); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_channel_state"})
			return nil
		}
		httpbridge.JSON(c, 200, fiber.Map{"channelUid": uid, "status": req.Status})
		return nil
	})
	router.Get("/admin-credentials", func(c fiber.Ctx) error {
		if !middleware.AuthorizeAdminAction(c, "admin.credentials", "") {
			return nil
		}
		views := make([]adminCredentialView, 0)
		for _, key := range cm.AdminCredentialsSnapshot() {
			views = append(views, publicAdminCredential(key))
		}
		httpbridge.JSON(c, 200, fiber.Map{"credentials": views})
		return nil
	})
	router.Post("/admin-credentials", func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if !middleware.AuthorizeAdminAction(c, "admin.credentials", "") {
			return nil
		}
		var req struct {
			adminCredentialView
			Key string `json:"key"`
		}
		if httpbridge.BindJSON(c, &req) != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_admin_credential"})
			return nil
		}
		raw := req.Key
		if raw == "" {
			token, err := randomEntryToken(32)
			if err != nil {
				return err
			}
			raw = "yqa_" + token
		}
		if env.IsStaticAdminAccessKey(raw) {
			httpbridge.JSON(c, 400, fiber.Map{"error": "environment_owner_is_immutable"})
			return nil
		}
		id, err := randomEntryToken(12)
		if err != nil {
			return err
		}
		actorID := req.ActorID
		if actorID == "" {
			actorID = "actor_" + id
		}
		key := config.AdminCredential{ID: "ak_" + id, KeyHash: config.EntryKeyHash(raw), ActorID: actorID, Role: req.Role, ChannelUIDs: req.ChannelUIDs, Disabled: req.Disabled, ExpiresAt: req.ExpiresAt}
		if err := cm.PutAdminCredentialAudited(managementAuditContext, key, ""); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_admin_credential"})
			return nil
		}
		httpbridge.Writer(c).Header().Set("Cache-Control", "no-store")
		httpbridge.JSON(c, 201, fiber.Map{"credential": publicAdminCredential(key), "key": raw})
		return nil
	})
	router.Put("/admin-credentials/:id", func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if !middleware.AuthorizeAdminAction(c, "admin.credentials", "") {
			return nil
		}
		var req adminCredentialView
		if httpbridge.BindJSON(c, &req) != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_admin_credential"})
			return nil
		}
		key, err := adminCredentialByID(cm, httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, 404, fiber.Map{"error": "credential_not_found"})
			return nil
		}
		if req.ActorID != "" && req.ActorID != key.ActorID {
			httpbridge.JSON(c, 400, fiber.Map{"error": "actor_is_immutable"})
			return nil
		}
		key.Role = req.Role
		key.ChannelUIDs = req.ChannelUIDs
		key.ExpiresAt = req.ExpiresAt
		key.Disabled = req.Disabled
		if err := cm.PutAdminCredentialAudited(managementAuditContext, key, ""); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_admin_credential"})
			return nil
		}
		httpbridge.JSON(c, 200, publicAdminCredential(key))
		return nil
	})
	router.Post("/admin-credentials/:id/rotate", func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		if !middleware.AuthorizeAdminAction(c, "admin.credentials", "") {
			return nil
		}
		key, err := adminCredentialByID(cm, httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, 404, fiber.Map{"error": "credential_not_found"})
			return nil
		}
		source := key.ID
		id, err := randomEntryToken(12)
		if err != nil {
			return err
		}
		token, err := randomEntryToken(32)
		if err != nil {
			return err
		}
		raw := "yqa_" + token
		key.ID = "ak_" + id
		key.KeyHash = config.EntryKeyHash(raw)
		key.Disabled = false
		if err := cm.PutAdminCredentialAudited(managementAuditContext, key, source); err != nil {
			return err
		}
		httpbridge.Writer(c).Header().Set("Cache-Control", "no-store")
		httpbridge.JSON(c, 201, fiber.Map{"credential": publicAdminCredential(key), "key": raw})
		return nil
	})
}
