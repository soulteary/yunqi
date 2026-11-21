package config

import (
	"errors"
	"time"
)

// AdminCredential authorizes management only; it never grants proxy access.
type AdminCredential struct {
	ID          string     `json:"id"`
	KeyHash     string     `json:"keyHash"`
	ActorID     string     `json:"actorId"`
	Role        string     `json:"role"`
	ChannelUIDs []string   `json:"channelUids,omitempty"`
	Disabled    bool       `json:"disabled,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
}

func (key AdminCredential) ValidNow(now time.Time) bool {
	return !key.Disabled && (key.ExpiresAt == nil || now.Before(*key.ExpiresAt)) && (key.Role == "admin" || key.Role == "operator" || key.Role == "viewer")
}
func cloneAdminCredential(key AdminCredential) AdminCredential {
	key.ChannelUIDs = append([]string(nil), key.ChannelUIDs...)
	if key.ExpiresAt != nil {
		expiry := *key.ExpiresAt
		key.ExpiresAt = &expiry
	}
	return key
}
func (cm *ConfigManager) AdminCredentialsSnapshot() []AdminCredential {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	keys := make([]AdminCredential, len(cm.config.AdminCredentials))
	for i, key := range cm.config.AdminCredentials {
		keys[i] = cloneAdminCredential(key)
	}
	return keys
}
func (cm *ConfigManager) FindAdminCredential(raw string) (AdminCredential, bool) {
	hash := EntryKeyHash(raw)
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	for _, key := range cm.config.AdminCredentials {
		if secureCompare(key.KeyHash, hash) {
			return cloneAdminCredential(key), true
		}
	}
	return AdminCredential{}, false
}
func (cm *ConfigManager) PutAdminCredential(key AdminCredential, rotateFrom string) error {
	if err := validateEntryKey(EntryKey{ID: key.ID, KeyHash: key.KeyHash, PrincipalID: key.ActorID, ChannelUIDs: key.ChannelUIDs}); err != nil {
		return err
	}
	if key.ID == "" || key.ActorID == "" || len(key.ID) > 128 || len(key.ActorID) > 128 || len(key.ChannelUIDs) > 256 || len(key.KeyHash) != 64 || (key.Role != "admin" && key.Role != "operator" && key.Role != "viewer") {
		return errors.New("invalid admin credential")
	}
	if rotateFrom != "" && rotateFrom == key.ID {
		return errors.New("rotation needs a fresh credential identity")
	}
	cm.mu.Lock()
	next := cm.config.deepCopy()
	found, oldFound := false, rotateFrom == ""
	for i, existing := range next.AdminCredentials {
		if existing.ID == rotateFrom {
			if existing.ActorID != key.ActorID {
				cm.mu.Unlock()
				return errors.New("rotation must retain actor")
			}
			next.AdminCredentials[i].Disabled = true
			oldFound = true
		}
		if existing.ID == key.ID {
			if existing.KeyHash != key.KeyHash || existing.ActorID != key.ActorID {
				cm.mu.Unlock()
				return errors.New("credential identity is immutable")
			}
			next.AdminCredentials[i] = cloneAdminCredential(key)
			found = true
		} else if existing.KeyHash == key.KeyHash {
			cm.mu.Unlock()
			return errors.New("credential already enrolled")
		}
	}
	if !oldFound {
		cm.mu.Unlock()
		return errors.New("rotation source missing")
	}
	if !found {
		if len(next.AdminCredentials) >= 4096 {
			cm.mu.Unlock()
			return errors.New("admin credential limit reached")
		}
		next.AdminCredentials = append(next.AdminCredentials, cloneAdminCredential(key))
	}
	if err := cm.saveConfigLocked(next); err != nil {
		cm.mu.Unlock()
		return err
	}
	cm.fireConfigChangeCallbacks()
	return nil
}

// SetChannelStatusByUID resolves and mutates under the same lock, so reorderings
// cannot change which resource a scoped administrator is allowed to operate.
func (cm *ConfigManager) SetChannelStatusByUID(uid, status string) error {
	if status != "active" && status != "suspended" && status != "disabled" {
		return errors.New("invalid channel state")
	}
	cm.mu.Lock()
	next := cm.config.deepCopy()
	var target *UpstreamConfig
	for _, kind := range []string{"messages", "chat", "responses", "gemini", "images", "vectors"} {
		if target = getChannelByUID(&next, kind, uid); target != nil {
			break
		}
	}
	if target == nil {
		cm.mu.Unlock()
		return errors.New("channel not found")
	}
	applyAdministrativeChannelStatus(target, status)
	if err := cm.saveConfigLocked(next); err != nil {
		cm.mu.Unlock()
		return err
	}
	cm.fireConfigChangeCallbacks()
	return nil
}
