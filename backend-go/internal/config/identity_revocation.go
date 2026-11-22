package config

import "context"

// IdentityRevocationBackend is authoritative across nodes only in explicit
// shared mode. Local configuration remains the default single-node ledger.
type IdentityRevocationBackend interface {
	Prepare(context.Context, string, bool) (string, error)
	IsRevoked(context.Context, string) (bool, error)
	Revoke(context.Context, string, int64) error
	Sync(context.Context, []IdentitySessionRevocation) error
	Close() error
}

func (cfg IdentityProxyConfig) EffectiveRevocationMode() string {
	if cfg.RevocationMode == "" {
		return "local"
	}
	return cfg.RevocationMode
}
