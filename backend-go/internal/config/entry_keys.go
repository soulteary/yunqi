package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"
	"time"
)

// EntryKey stores only a digest. Disabled records are retained so an enrolled
// legacy environment key cannot regain access through the static fallback.
type EntryKey struct {
	ID          string     `json:"id"`
	KeyHash     string     `json:"keyHash"`
	PrincipalID string     `json:"principalId"`
	Models      []string   `json:"models,omitempty"`
	ChannelUIDs []string   `json:"channelUids,omitempty"`
	CIDRs       []string   `json:"cidrs,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	Disabled    bool       `json:"disabled,omitempty"`
}

func EntryKeyHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
func cloneEntryKey(key EntryKey) EntryKey {
	key.Models = append([]string(nil), key.Models...)
	key.ChannelUIDs = append([]string(nil), key.ChannelUIDs...)
	key.CIDRs = append([]string(nil), key.CIDRs...)
	if key.ExpiresAt != nil {
		expiry := *key.ExpiresAt
		key.ExpiresAt = &expiry
	}
	return key
}
func (key EntryKey) ValidNow(now time.Time) bool {
	return !key.Disabled && (key.ExpiresAt == nil || now.Before(*key.ExpiresAt))
}
func (key EntryKey) AllowsModel(model string) bool {
	if len(key.Models) == 0 {
		return true
	}
	for _, pattern := range key.Models {
		if pattern == model || strings.HasSuffix(pattern, "*") && strings.HasPrefix(model, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}
func (key EntryKey) AllowsChannel(uid string) bool {
	if len(key.ChannelUIDs) == 0 {
		return true
	}
	for _, allowed := range key.ChannelUIDs {
		if allowed == uid {
			return true
		}
	}
	return false
}
func (key EntryKey) AllowsIP(ip string) bool {
	if len(key.CIDRs) == 0 {
		return true
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, raw := range key.CIDRs {
		prefix, err := netip.ParsePrefix(raw)
		if err == nil && prefix.Contains(addr) {
			return true
		}
	}
	return false
}
func validateEntryKey(key EntryKey) error {
	if key.ID == "" || len(key.ID) > 128 || key.PrincipalID == "" || len(key.PrincipalID) > 128 {
		return errors.New("invalid entry key identity")
	}
	decoded, err := hex.DecodeString(key.KeyHash)
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("invalid key digest")
	}
	if len(key.Models) > 256 || len(key.ChannelUIDs) > 256 || len(key.CIDRs) > 256 {
		return errors.New("too many policy rules")
	}
	for _, pattern := range key.Models {
		if pattern == "" || len(pattern) > 512 || strings.Contains(strings.TrimSuffix(pattern, "*"), "*") {
			return errors.New("model rules support only a trailing wildcard")
		}
	}
	for _, raw := range key.CIDRs {
		if _, err := netip.ParsePrefix(raw); err != nil {
			return errors.New("invalid CIDR")
		}
	}
	return nil
}
func (cm *ConfigManager) EntryKeysSnapshot() []EntryKey {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	keys := make([]EntryKey, len(cm.config.EntryKeys))
	for i, key := range cm.config.EntryKeys {
		keys[i] = cloneEntryKey(key)
	}
	return keys
}
func (cm *ConfigManager) FindEntryKey(raw string) (EntryKey, bool) {
	hash := EntryKeyHash(raw)
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	for _, key := range cm.config.EntryKeys {
		if secureCompare(key.KeyHash, hash) {
			return cloneEntryKey(key), true
		}
	}
	return EntryKey{}, false
}

// PutEntryKey atomically persists a policy or a rotation. Rotation disables the
// old credential and installs a fresh digest with the same stable principal.
func (cm *ConfigManager) PutEntryKey(key EntryKey, rotateFrom string) error {
	if rotateFrom != "" && rotateFrom == key.ID {
		return errors.New("rotation requires a new key identity")
	}
	if err := validateEntryKey(key); err != nil {
		return err
	}
	cm.mu.Lock()
	next := append([]EntryKey(nil), cm.config.EntryKeys...)
	found := false
	oldFound := rotateFrom == ""
	for i, existing := range next {
		if existing.ID == rotateFrom {
			if existing.PrincipalID != key.PrincipalID {
				cm.mu.Unlock()
				return errors.New("rotation must retain principal")
			}
			next[i].Disabled = true
			oldFound = true
		}
		if existing.ID == key.ID {
			if existing.KeyHash != key.KeyHash {
				cm.mu.Unlock()
				return errors.New("use rotation to replace a key digest")
			}
			next[i] = cloneEntryKey(key)
			found = true
		} else if existing.KeyHash == key.KeyHash {
			cm.mu.Unlock()
			return errors.New("key digest already enrolled")
		}
	}
	if !oldFound {
		cm.mu.Unlock()
		return errors.New("rotation source missing")
	}
	if !found {
		if len(next) >= 4096 {
			cm.mu.Unlock()
			return errors.New("entry key limit reached")
		}
		next = append(next, cloneEntryKey(key))
	}
	previous := cm.config.EntryKeys
	cm.config.EntryKeys = next
	if err := cm.saveConfigLocked(cm.config); err != nil {
		cm.config.EntryKeys = previous
		cm.mu.Unlock()
		return err
	}
	cm.fireConfigChangeCallbacks()
	return nil
}
