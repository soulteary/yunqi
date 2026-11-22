package config

import (
	"encoding/hex"
	"errors"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type IdentityGroupMapping struct {
	Group       string   `json:"group"`
	Role        string   `json:"role"`
	ChannelUIDs []string `json:"channelUids,omitempty"`
}

type IdentitySessionRevocation struct {
	Hash      string `json:"hash"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
}

// IdentityProxyConfig authorizes assertions only from configured peer networks
// plus an environment-held shared secret. Group/revocation policy is also used
// by native OIDC without enabling trusted-proxy assertions.
type IdentityProxyConfig struct {
	RevocationEpoch   string                      `json:"revocationEpoch,omitempty"`
	RevocationMode    string                      `json:"revocationMode,omitempty"`
	RevokedSessions   []IdentitySessionRevocation `json:"revokedSessions,omitempty"`
	AllowedOrigins    []string                    `json:"allowedOrigins,omitempty"`
	Enabled           bool                        `json:"enabled"`
	Issuer            string                      `json:"issuer"`
	TrustedCIDRs      []string                    `json:"trustedCidrs,omitempty"`
	MaxSessionSeconds int64                       `json:"maxSessionSeconds,omitempty"`
	GroupMappings     []IdentityGroupMapping      `json:"groupMappings,omitempty"`
}

func cloneIdentityProxy(cfg IdentityProxyConfig) IdentityProxyConfig {
	cfg.RevokedSessions = append([]IdentitySessionRevocation(nil), cfg.RevokedSessions...)
	cfg.AllowedOrigins = append([]string(nil), cfg.AllowedOrigins...)
	cfg.TrustedCIDRs = append([]string(nil), cfg.TrustedCIDRs...)
	cfg.GroupMappings = append([]IdentityGroupMapping(nil), cfg.GroupMappings...)
	for i := range cfg.GroupMappings {
		cfg.GroupMappings[i].ChannelUIDs = append([]string(nil), cfg.GroupMappings[i].ChannelUIDs...)
	}
	return cfg
}
func (cm *ConfigManager) IdentityProxySnapshot() IdentityProxyConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if cm.config.IdentityProxy == nil {
		return IdentityProxyConfig{}
	}
	return cloneIdentityProxy(*cm.config.IdentityProxy)
}
func (cm *ConfigManager) SetIdentityProxyConfigAudited(auditContext ManagementAuditContext, cfg IdentityProxyConfig) error {
	auditContext = auditContext.normalized()

	cfg = cloneIdentityProxy(cfg)
	cfg.RevocationEpoch = strings.ToLower(cfg.RevocationEpoch)
	if cfg.RevocationEpoch != "" {
		decoded, err := hex.DecodeString(cfg.RevocationEpoch)
		if err != nil || len(decoded) != 32 {
			return errors.New("invalid identity revocation epoch")
		}
	}

	if cfg.RevocationMode != "" && cfg.RevocationMode != "local" && cfg.RevocationMode != "shared" {
		return errors.New("invalid identity revocation mode")
	}
	if len(cfg.AllowedOrigins) > 32 || len(cfg.TrustedCIDRs) > 256 || len(cfg.GroupMappings) > 256 || len(cfg.Issuer) > 128 || cfg.MaxSessionSeconds < 0 || cfg.MaxSessionSeconds > 30*24*60*60 {
		return errors.New("invalid identity proxy configuration")
	}
	if cfg.Enabled && (strings.TrimSpace(cfg.Issuer) == "" || len(cfg.TrustedCIDRs) == 0 || len(cfg.GroupMappings) == 0 || len(cfg.AllowedOrigins) == 0) {
		return errors.New("identity proxy needs issuer, trusted peers and group mappings")
	}
	for i, raw := range cfg.AllowedOrigins {
		normalized, err := NormalizeIdentityOrigin(raw)
		if err != nil {
			return err
		}
		cfg.AllowedOrigins[i] = normalized
	}
	for _, raw := range cfg.TrustedCIDRs {
		if _, err := netip.ParsePrefix(raw); err != nil {
			return errors.New("invalid identity proxy peer CIDR")
		}
	}
	seen := make(map[string]bool)
	for _, mapping := range cfg.GroupMappings {
		if mapping.Group == "" || len(mapping.Group) > 128 || strings.ContainsAny(mapping.Group, ",\r\n") || seen[mapping.Group] || len(mapping.ChannelUIDs) > 256 || (mapping.Role != "viewer" && mapping.Role != "operator" && mapping.Role != "admin") {
			return errors.New("invalid identity proxy group mapping")
		}
		seen[mapping.Group] = true
	}
	cm.mu.Lock()
	next := cm.config.deepCopy()
	// Omitted mode from an older client preserves the current scope atomically.
	// Only an explicit "local" opts out of shared revocation.
	if cfg.RevocationMode == "" {
		cfg.RevocationMode = "local"
		if next.IdentityProxy != nil {
			cfg.RevocationMode = next.IdentityProxy.EffectiveRevocationMode()
		}
	}
	// A bound realm survives explicit local mode and omitted legacy fields.
	// API policy edits cannot reset a missing/corrupt shared revocation realm.
	if next.IdentityProxy != nil && next.IdentityProxy.RevocationEpoch != "" {
		previous := strings.ToLower(next.IdentityProxy.RevocationEpoch)
		if cfg.RevocationEpoch != "" && cfg.RevocationEpoch != previous {
			cm.mu.Unlock()
			return errors.New("identity revocation epoch is immutable")
		}
		cfg.RevocationEpoch = previous
	}
	// Ordinary policy edits never silently revive revoked sessions.
	cfg.RevokedSessions = nil
	if next.IdentityProxy != nil {
		for _, record := range next.IdentityProxy.RevokedSessions {
			if record.ExpiresAt == 0 || record.ExpiresAt > time.Now().Unix() {
				cfg.RevokedSessions = append(cfg.RevokedSessions, record)
			}
		}
	}
	copy := cloneIdentityProxy(cfg)
	next.IdentityProxy = &copy
	if err := cm.saveConfigLockedAudited(auditContext, next); err != nil {
		cm.mu.Unlock()
		return err
	}
	cm.fireConfigChangeCallbacks()
	return nil
}
func (cm *ConfigManager) RevokeIdentitySessionAudited(auditContext ManagementAuditContext, hash string) error {
	auditContext = auditContext.normalized()

	return cm.RevokeIdentitySessionUntilAudited(auditContext, hash, 0)
}
func (cm *ConfigManager) RevokeIdentitySessionUntilAudited(auditContext ManagementAuditContext, hash string, expiresAt int64) error {
	auditContext = auditContext.normalized()

	return cm.revokeIdentitySessionUntilAtAudited(auditContext, hash, expiresAt, time.Now().Unix())
}
func (cm *ConfigManager) revokeIdentitySessionUntilAtAudited(auditContext ManagementAuditContext, hash string, expiresAt, now int64) error {
	auditContext = auditContext.normalized()

	hash = strings.ToLower(hash)
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != 32 || expiresAt < 0 {
		return errors.New("invalid session fingerprint")
	}
	if expiresAt > 0 && expiresAt <= now {
		return nil
	}
	cm.mu.Lock()
	next := cm.config.deepCopy()
	if next.IdentityProxy == nil {
		cm.mu.Unlock()
		return errors.New("identity proxy not configured")
	}
	active := make([]IdentitySessionRevocation, 0, len(next.IdentityProxy.RevokedSessions)+1)
	found := false
	for _, record := range next.IdentityProxy.RevokedSessions {
		if record.ExpiresAt > 0 && record.ExpiresAt <= now {
			continue
		}
		if record.Hash == hash {
			found = true
			if record.ExpiresAt != 0 && (expiresAt == 0 || expiresAt > record.ExpiresAt) {
				record.ExpiresAt = expiresAt
			}
		}
		active = append(active, record)
	}
	if !found {
		if len(active) >= 4096 {
			cm.mu.Unlock()
			return errors.New("session revocation limit reached")
		}
		active = append(active, IdentitySessionRevocation{Hash: hash, ExpiresAt: expiresAt})
	}
	next.IdentityProxy.RevokedSessions = active
	if err := cm.saveConfigLockedAudited(auditContext, next); err != nil {
		cm.mu.Unlock()
		return err
	}
	cm.fireConfigChangeCallbacks()
	return nil
}

func IdentitySessionHash(issuer, session string) string {
	return EntryKeyHash(issuer + "\x00" + session)
}

// NormalizeIdentityOrigin accepts concrete web origins, never wildcard/prefix
// matches or a forwarded Host assertion from an untrusted request.
func NormalizeIdentityOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Hostname() == "" || strings.Contains(u.Host, "*") || (u.Scheme != "https" && u.Scheme != "http") || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || strings.ContainsAny(raw, "#\r\n") {
		return "", errors.New("invalid identity proxy origin")
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

// RevokeIdentitySession preserves the system/background call contract. HTTP callers use RevokeIdentitySessionAudited.
func (cm *ConfigManager) RevokeIdentitySession(hash string) error {
	return cm.RevokeIdentitySessionAudited(ManagementAuditContext{}, hash)
}

// RevokeIdentitySessionUntil preserves the system/background call contract. HTTP callers use RevokeIdentitySessionUntilAudited.
func (cm *ConfigManager) RevokeIdentitySessionUntil(hash string, expiresAt int64) error {
	return cm.RevokeIdentitySessionUntilAudited(ManagementAuditContext{}, hash, expiresAt)
}

// SetIdentityProxyConfig preserves the system/background call contract. HTTP callers use SetIdentityProxyConfigAudited.
func (cm *ConfigManager) SetIdentityProxyConfig(cfg IdentityProxyConfig) error {
	return cm.SetIdentityProxyConfigAudited(ManagementAuditContext{}, cfg)
}

// revokeIdentitySessionUntilAt preserves the system/background call contract. HTTP callers use revokeIdentitySessionUntilAtAudited.
func (cm *ConfigManager) revokeIdentitySessionUntilAt(hash string, expiresAt, now int64) error {
	return cm.revokeIdentitySessionUntilAtAudited(ManagementAuditContext{}, hash, expiresAt, now)
}
