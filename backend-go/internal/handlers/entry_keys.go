package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

type entryKeyView struct {
	ID          string     `json:"id"`
	PrincipalID string     `json:"principalId"`
	Models      []string   `json:"models,omitempty"`
	ChannelUIDs []string   `json:"channelUids,omitempty"`
	CIDRs       []string   `json:"cidrs,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	Disabled    bool       `json:"disabled"`
}

func entryKeyPublic(key config.EntryKey) entryKeyView {
	return entryKeyView{key.ID, key.PrincipalID, key.Models, key.ChannelUIDs, key.CIDRs, key.ExpiresAt, key.Disabled}
}
func randomEntryToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// RegisterEntryKeyRoutes exposes admin-only lifecycle operations through the
// existing /api management authentication. Secrets are returned only on create.
func RegisterEntryKeyRoutes(router fiber.Router, cm *config.ConfigManager) {
	router.Get("/entry-keys", func(c fiber.Ctx) error {
		views := make([]entryKeyView, 0)
		for _, key := range cm.EntryKeysSnapshot() {
			views = append(views, entryKeyPublic(key))
		}
		httpbridge.JSON(c, 200, fiber.Map{"keys": views})
		return nil
	})
	router.Post("/entry-keys", func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		var req struct {
			entryKeyView
			Key string `json:"key"`
		}
		if httpbridge.BindJSON(c, &req) != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_entry_key"})
			return nil
		}
		raw := req.Key
		if raw == "" {
			token, err := randomEntryToken(32)
			if err != nil {
				return err
			}
			raw = "yq_" + token
		}
		id, err := randomEntryToken(12)
		if err != nil {
			return err
		}
		key := config.EntryKey{ID: "ek_" + id, KeyHash: config.EntryKeyHash(raw), PrincipalID: req.PrincipalID, Models: req.Models, ChannelUIDs: req.ChannelUIDs, CIDRs: req.CIDRs, ExpiresAt: req.ExpiresAt, Disabled: req.Disabled}
		if err := cm.PutEntryKeyAudited(managementAuditContext, key, ""); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_entry_key_policy"})
			return nil
		}
		httpbridge.JSON(c, 201, fiber.Map{"entryKey": entryKeyPublic(key), "key": raw})
		return nil
	})
	router.Put("/entry-keys/:id", func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		var req entryKeyView
		if httpbridge.BindJSON(c, &req) != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_entry_key"})
			return nil
		}
		key, err := findEntryKeyByID(cm, httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, 404, fiber.Map{"error": "entry_key_not_found"})
			return nil
		}
		// A key identity's subject remains stable; use an explicit new credential
		// rather than moving an existing credential between budget ledgers.
		if req.PrincipalID != "" && req.PrincipalID != key.PrincipalID {
			httpbridge.JSON(c, 400, fiber.Map{"error": "principal_is_immutable"})
			return nil
		}
		key.Models = req.Models
		key.ChannelUIDs = req.ChannelUIDs
		key.CIDRs = req.CIDRs
		key.ExpiresAt = req.ExpiresAt
		key.Disabled = req.Disabled
		if err := cm.PutEntryKeyAudited(managementAuditContext, key, ""); err != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "invalid_entry_key_policy"})
			return nil
		}
		httpbridge.JSON(c, 200, entryKeyPublic(key))
		return nil
	})
	router.Post("/entry-keys/:id/rotate", func(c fiber.Ctx) error {
		managementAuditContext := config.ManagementAuditFromContext(httpbridge.Request(c).Context())

		key, err := findEntryKeyByID(cm, httpbridge.Param(c, "id"))
		if err != nil {
			httpbridge.JSON(c, 404, fiber.Map{"error": "entry_key_not_found"})
			return nil
		}
		source := key.ID
		token, err := randomEntryToken(32)
		if err != nil {
			return err
		}
		raw := "yq_" + token
		id, err := randomEntryToken(12)
		if err != nil {
			return err
		}
		key.ID = "ek_" + id
		key.KeyHash = config.EntryKeyHash(raw)
		key.Disabled = false
		if err := cm.PutEntryKeyAudited(managementAuditContext, key, source); err != nil {
			return err
		}
		httpbridge.JSON(c, 201, fiber.Map{"entryKey": entryKeyPublic(key), "key": raw})
		return nil
	})
}
func findEntryKeyByID(cm *config.ConfigManager, id string) (config.EntryKey, error) {
	for _, key := range cm.EntryKeysSnapshot() {
		if key.ID == id {
			return key, nil
		}
	}
	return config.EntryKey{}, errors.New("entry key missing")
}
