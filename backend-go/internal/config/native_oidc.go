package config

import "context"

// NativeIdentitySession contains no upstream tokens or clear-text subject.
type NativeIdentitySession struct {
	SubjectID   string
	Groups      []string
	IssuedAt    int64
	ExpiresAt   int64
	Fingerprint string
}

type NativeIdentityBackend interface {
	Lookup(context.Context, string) (NativeIdentitySession, error)
	Close() error
}
