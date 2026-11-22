package metrics

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

const UnknownPrincipal = "unknown"

var attributionIDPattern = regexp.MustCompile(`^[pk]_[a-f0-9]{64}$`)

// PrincipalAttributionID records a domain-separated opaque authenticated identity,
// never a supplied header, raw key, subject, email, display mask or user label.
// p_ identities survive key rotation when a stable principal is configured;
// k_ identities are credential-bound and cannot infer a team after rotation.
func PrincipalAttributionID(authenticated string) string {
	prefix := ""
	switch {
	case strings.HasPrefix(authenticated, "principal:") && len(authenticated) > len("principal:"):
		prefix = "p_"
	case strings.HasPrefix(authenticated, "key:") && len(authenticated) > len("key:"):
		prefix = "k_"
	default:
		return ""
	}
	hash := sha256.Sum256([]byte("yunqi.cost.principal.v1\x00" + authenticated))
	return prefix + hex.EncodeToString(hash[:])
}
func ValidPrincipalAttributionID(id string) bool {
	return id == UnknownPrincipal || attributionIDPattern.MatchString(id)
}
func storedPrincipalID(id string) string {
	if attributionIDPattern.MatchString(id) {
		return id
	}
	return ""
}
func publicPrincipalID(id string) string {
	if storedPrincipalID(id) == "" {
		return UnknownPrincipal
	}
	return id
}
func PrincipalAttributionLabel(id string) string {
	if !attributionIDPattern.MatchString(id) {
		return UnknownPrincipal
	}
	kind := "principal:"
	if strings.HasPrefix(id, "k_") {
		kind = "credential:"
	}
	return kind + id[2:10] + "..." + id[len(id)-4:]
}
