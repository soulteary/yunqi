package middleware

import (
	"crypto/subtle"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

const (
	IdentitySecretHeader     = "X-Yunqi-Identity-Secret"
	IdentitySubjectHeader    = "X-Yunqi-Identity-Subject"
	IdentityGroupsHeader     = "X-Yunqi-Identity-Groups"
	IdentitySessionHeader    = "X-Yunqi-Identity-Session"
	IdentityExpiresHeader    = "X-Yunqi-Identity-Expires"
	IdentityManagementHeader = "X-Yunqi-Management-Request"
)

func validIdentityText(value string, limit int) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// StripIdentityProxyHeaders keeps trusted assertions and their shared secret
// inside the management authentication boundary, never an upstream request.
func StripIdentityProxyHeaders(c fiber.Ctx) {
	for name := range httpbridge.Request(c).Header {
		if strings.HasPrefix(strings.ToLower(name), "x-yunqi-identity-") || strings.EqualFold(name, IdentityManagementHeader) {
			delete(httpbridge.Request(c).Header, name)
			c.Request().Header.Del(name)
		}
	}
}

func authenticateTrustedIdentity(c fiber.Ctx, env *config.EnvConfig) bool {
	if env.IdentityProxyConfigLookup == nil || len(env.IdentityProxySecret) < 32 {
		return false
	}
	cfg := env.IdentityProxyConfigLookup()
	if !cfg.Enabled || cfg.Issuer == "" || len(cfg.TrustedCIDRs) == 0 || len(cfg.GroupMappings) == 0 || len(cfg.AllowedOrigins) == 0 {
		return false
	}
	if !(config.EntryKey{CIDRs: cfg.TrustedCIDRs}).AllowsIP(peerIP(c)) {
		return false
	}
	secret := httpbridge.GetHeader(c, IdentitySecretHeader)
	if len(secret) > 1024 || subtle.ConstantTimeCompare([]byte(secret), []byte(env.IdentityProxySecret)) != 1 {
		return false
	}
	if httpbridge.GetHeader(c, IdentityManagementHeader) != "1" {
		return false
	}
	// Ambient proxy sessions require a concrete allowed browser origin on
	// every write, plus a non-simple request marker that HTML forms cannot set.
	origin := httpbridge.GetHeader(c, "Origin")
	originAllowed := false
	if origin != "" {
		normalized, err := config.NormalizeIdentityOrigin(origin)
		if err != nil {
			return false
		}
		for _, allowed := range cfg.AllowedOrigins {
			candidate, err := config.NormalizeIdentityOrigin(allowed)
			if err == nil && candidate == normalized {
				originAllowed = true
				break
			}
		}
		if !originAllowed {
			return false
		}
	}
	method := httpbridge.Request(c).Method
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		if !originAllowed || httpbridge.GetHeader(c, IdentityManagementHeader) != "1" {
			return false
		}
	}
	subject := httpbridge.GetHeader(c, IdentitySubjectHeader)
	session := httpbridge.GetHeader(c, IdentitySessionHeader)
	if !validIdentityText(subject, 256) || !validIdentityText(session, 256) {
		return false
	}
	expires, err := strconv.ParseInt(httpbridge.GetHeader(c, IdentityExpiresHeader), 10, 64)
	now := time.Now().Unix()
	maxSession := cfg.MaxSessionSeconds
	if maxSession <= 0 {
		maxSession = 86400
	}
	if maxSession > 30*86400 {
		maxSession = 30 * 86400
	}
	if err != nil || expires <= now || expires-now > maxSession {
		return false
	}
	fingerprint := config.IdentitySessionHash(cfg.Issuer, session)
	for _, record := range cfg.RevokedSessions {
		if record.Hash == fingerprint && (record.ExpiresAt == 0 || record.ExpiresAt > now) {
			return false
		}
	}

	rawGroups := httpbridge.GetHeader(c, IdentityGroupsHeader)
	if len(rawGroups) > 4096 {
		return false
	}
	groups := strings.Split(rawGroups, ",")
	if len(groups) > 64 {
		return false
	}
	present := make(map[string]bool)
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group != "" {
			present[group] = true
		}
	}
	rank := func(role string) int {
		switch role {
		case "viewer":
			return 1
		case "operator":
			return 2
		case "admin":
			return 3
		}
		return 0
	}
	chosenRole := ""
	chosenRank := 0
	allResources := false
	scopes := make(map[string]bool)
	for _, mapping := range cfg.GroupMappings {
		currentRank := rank(mapping.Role)
		if !present[mapping.Group] || currentRank == 0 || currentRank < chosenRank {
			continue
		}
		if currentRank > chosenRank {
			chosenRole = mapping.Role
			chosenRank = currentRank
			scopes = make(map[string]bool)
			allResources = false
		}
		if len(mapping.ChannelUIDs) == 0 {
			allResources = true
		}
		for _, uid := range mapping.ChannelUIDs {
			scopes[uid] = true
		}
	}
	if chosenRank == 0 || len(scopes) > 256 {
		return false
	}
	var channelUIDs []string
	if !allResources {
		for uid := range scopes {
			channelUIDs = append(channelUIDs, uid)
		}
	}
	sort.Strings(channelUIDs)
	SetAdminPrincipal(c, AdminPrincipal{ID: "sso:" + config.EntryKeyHash(cfg.Issuer+"\x00"+subject), Role: chosenRole, ChannelUIDs: channelUIDs, Source: "identity_proxy", SessionFingerprint: fingerprint, SessionExpiresAt: expires})
	return true
}
