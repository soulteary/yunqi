// Package nativeoidc implements the management-only OIDC code flow. Browser
// cookies are opaque references to server state, never ID/access/refresh tokens.
package nativeoidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpclient"
	"golang.org/x/oauth2"
)

const CookieName = "__Host-yunqi-session"
const FlowCookieName = "__Host-yunqi-oidc-flow"

type Manager struct {
	env             *config.EnvConfig
	client          *http.Client
	store           *storage
	origin          string
	realm           string
	policy          string
	revocationEpoch string
}
type flow struct {
	Policy   string
	Epoch    string
	Nonce    string
	Verifier string
	Browser  string
	Expires  int64
}
type session struct {
	config.NativeIdentitySession
	Policy string
	Epoch  string
}

func New(env *config.EnvConfig) (*Manager, error) {
	issuer, err := url.Parse(env.NativeOIDCIssuer)
	if err != nil || issuer.Scheme != "https" || issuer.Hostname() == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" || len(env.NativeOIDCIssuer) > 2048 {
		return nil, errInvalid
	}
	redirect, err := url.Parse(env.NativeOIDCRedirectURL)
	if err != nil || redirect.Scheme != "https" || redirect.Hostname() == "" || redirect.User != nil || redirect.Path != "/auth/oidc/callback" || redirect.RawQuery != "" || redirect.Fragment != "" || len(env.NativeOIDCClientID) == 0 || len(env.NativeOIDCClientID) > 256 || len(env.NativeOIDCClientSecret) > 4096 {
		return nil, errInvalid
	}
	if len(split(env.NativeOIDCRequiredAMR)) == 0 && len(split(env.NativeOIDCAllowedACR)) == 0 {
		return nil, errInvalid
	}
	if len(env.NativeOIDCRequiredAMR) > 1024 || len(env.NativeOIDCAllowedACR) > 1024 {
		return nil, errInvalid
	}
	realm := identityPolicyHash(env)
	rawURL := ""
	if env.IdentityProxyConfigLookup == nil {
		return nil, errInvalid
	}
	cfg := env.IdentityProxyConfigLookup()
	if cfg.EffectiveRevocationMode() == "shared" {
		if env.IdentityProxyRevocations == nil || cfg.RevocationEpoch == "" || env.IdentityProxyRevocationRedisURL == "" {
			return nil, errUnavailable
		}
		rawURL = env.IdentityProxyRevocationRedisURL
		realm = config.EntryKeyHash(realm + "\x00" + cfg.RevocationEpoch)
	}
	store, err := newStorage(rawURL, env.IdentityProxyRevocationRedisNamespace+":oidc:"+realm+":")
	if err != nil {
		return nil, err
	}
	client := *httpclient.GetManager().GetStandardClient(10*time.Second, false)
	// OIDC must not redirect a confidential token POST to another endpoint. The
	// policy-aware transport still validates every discovery/token/JWKS request.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Manager{env: env, client: &client, store: store, realm: realm, origin: redirect.Scheme + "://" + redirect.Host, policy: identityPolicyHash(env), revocationEpoch: cfg.RevocationEpoch}, nil
}
func (m *Manager) Close() error   { return m.store.close() }
func (m *Manager) Origin() string { return m.origin }
func (m *Manager) provider(ctx context.Context) (*oidc.Provider, context.Context, error) {
	ctx = oidc.ClientContext(ctx, m.client)
	p, err := oidc.NewProvider(ctx, m.env.NativeOIDCIssuer)
	if err != nil {
		return nil, ctx, errUnavailable
	}
	// Require TLS independently from the configurable network destination policy.
	var metadata struct {
		Auth  string `json:"authorization_endpoint"`
		Token string `json:"token_endpoint"`
		JWKS  string `json:"jwks_uri"`
	}
	if p.Claims(&metadata) != nil {
		return nil, ctx, errInvalid
	}
	for _, raw := range []string{metadata.Auth, metadata.Token, metadata.JWKS} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return nil, ctx, errInvalid
		}
	}
	return p, ctx, nil
}
func (m *Manager) Begin(ctx context.Context) (authURL, browser string, err error) {
	if !m.unchanged() {
		return "", "", errUnavailable
	}
	state, err := random()
	if err != nil {
		return "", "", err
	}
	nonce, err := random()
	if err != nil {
		return "", "", err
	}
	verifier, err := random()
	if err != nil {
		return "", "", err
	}
	browser, err = random()
	if err != nil {
		return "", "", err
	}
	f := flow{Policy: m.policy, Epoch: m.revocationEpoch, Nonce: nonce, Verifier: verifier, Browser: hash(browser), Expires: time.Now().Add(5 * time.Minute).Unix()}
	if err = m.store.put(ctx, "flow:"+hash(state), marshal(f), "", f.Expires); err != nil {
		return "", "", err
	}
	p, _, err := m.provider(ctx)
	if err != nil {
		_, _ = m.store.get(ctx, "flow:"+hash(state), "", true)
		return "", "", err
	}
	oauth := m.oauth(p)
	options := []oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
	if m.env.NativeOIDCAllowedACR != "" {
		options = append(options, oauth2.SetAuthURLParam("acr_values", strings.Join(split(m.env.NativeOIDCAllowedACR), " ")))
	}
	return oauth.AuthCodeURL(state, options...), browser, nil
}
func (m *Manager) oauth(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{ClientID: m.env.NativeOIDCClientID, ClientSecret: m.env.NativeOIDCClientSecret, RedirectURL: m.env.NativeOIDCRedirectURL, Endpoint: p.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "groups"}}
}
func (m *Manager) Complete(ctx context.Context, state, code, browser string) (string, error) {
	if !m.unchanged() {
		return "", errUnavailable
	}
	if len(state) != 43 || len(browser) != 43 || len(code) == 0 || len(code) > 4096 {
		return "", errInvalid
	}
	raw, err := m.store.get(ctx, "flow:"+hash(state), "", true)
	if err != nil {
		return "", err
	}
	var f flow
	if json.Unmarshal([]byte(raw), &f) != nil || f.Policy != m.policy || f.Epoch != m.revocationEpoch || f.Expires <= time.Now().Unix() || subtle.ConstantTimeCompare([]byte(f.Browser), []byte(hash(browser))) != 1 {
		return "", errInvalid
	}
	p, ctx, err := m.provider(ctx)
	if err != nil {
		return "", err
	}
	oauth := m.oauth(p)
	token, err := oauth.Exchange(ctx, code, oauth2.VerifierOption(f.Verifier))
	if err != nil {
		return "", errInvalid
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok || len(rawID) > 65536 {
		return "", errInvalid
	}
	verified, err := p.Verifier(&oidc.Config{ClientID: m.env.NativeOIDCClientID, SupportedSigningAlgs: []string{"RS256", "ES256", "EdDSA"}}).Verify(ctx, rawID)
	if err != nil || verified.IssuedAt.IsZero() || verified.IssuedAt.After(time.Now().Add(30*time.Second)) || verified.Subject == "" || len(verified.Subject) > 256 || subtle.ConstantTimeCompare([]byte(verified.Nonce), []byte(f.Nonce)) != 1 {
		return "", errInvalid
	}
	var claims struct {
		Groups          []string `json:"groups"`
		AMR             []string `json:"amr"`
		ACR             string   `json:"acr"`
		AuthorizedParty string   `json:"azp"`
	}
	if verified.Claims(&claims) != nil || len(claims.Groups) > 64 || !assurance(claims.AMR, claims.ACR, m.env.NativeOIDCRequiredAMR, m.env.NativeOIDCAllowedACR) {
		return "", errInvalid
	}
	if (len(verified.Audience) > 1 || claims.AuthorizedParty != "") && claims.AuthorizedParty != m.env.NativeOIDCClientID {
		return "", errInvalid
	}
	for _, g := range claims.Groups {
		if g == "" || len(g) > 128 || strings.ContainsAny(g, ",\r\n") {
			return "", errInvalid
		}
	}
	cfg := m.env.IdentityProxyConfigLookup()
	if !hasMapping(cfg, claims.Groups) {
		return "", errInvalid
	}
	randomID, err := random()
	if err != nil {
		return "", err
	}
	subject := config.EntryKeyHash(m.env.NativeOIDCIssuer + "\x00" + verified.Subject)
	cookie := subject + "." + randomID
	issuedAt := time.Now().Unix()
	maxSession := int64(8 * 60 * 60)
	if cfg.MaxSessionSeconds > 0 {
		maxSession = min(maxSession, cfg.MaxSessionSeconds)
	}
	expires := min(verified.Expiry.Unix(), issuedAt+maxSession)
	s := session{NativeIdentitySession: config.NativeIdentitySession{SubjectID: subject, Groups: claims.Groups, IssuedAt: issuedAt, ExpiresAt: expires, Fingerprint: config.IdentitySessionHash(m.env.NativeOIDCIssuer, cookie)}, Policy: m.policy, Epoch: m.revocationEpoch}
	if err = m.checkRevocation(ctx, s.Fingerprint); err != nil {
		return "", err
	}
	if err = m.store.put(ctx, "session:"+hash(cookie), marshal(s), subject, expires); err != nil {
		return "", err
	}
	return cookie, nil
}
func (m *Manager) Lookup(ctx context.Context, cookie string) (config.NativeIdentitySession, error) {
	if !m.unchanged() {
		return config.NativeIdentitySession{}, errUnavailable
	}
	subject, token, ok := strings.Cut(cookie, ".")
	if !ok || len(subject) != 64 || len(token) != 43 {
		return config.NativeIdentitySession{}, errInvalid
	}
	if _, err := hex.DecodeString(subject); err != nil {
		return config.NativeIdentitySession{}, errInvalid
	}
	raw, err := m.store.get(ctx, "session:"+hash(cookie), subject, false)
	if err != nil {
		return config.NativeIdentitySession{}, err
	}
	var s session
	if json.Unmarshal([]byte(raw), &s) != nil || s.Policy != m.policy || s.Epoch != m.revocationEpoch || s.SubjectID != subject || s.ExpiresAt <= time.Now().Unix() || s.Fingerprint != config.IdentitySessionHash(m.env.NativeOIDCIssuer, cookie) {
		return config.NativeIdentitySession{}, errInvalid
	}
	cfg := m.env.IdentityProxyConfigLookup()
	if cfg.MaxSessionSeconds > 0 {
		s.ExpiresAt = min(s.ExpiresAt, s.IssuedAt+cfg.MaxSessionSeconds)
	}
	if s.IssuedAt <= 0 || s.ExpiresAt <= time.Now().Unix() {
		return config.NativeIdentitySession{}, errInvalid
	}
	if err = m.checkRevocation(ctx, s.Fingerprint); err != nil {
		return config.NativeIdentitySession{}, err
	}
	return s.NativeIdentitySession, nil
}
func (m *Manager) checkRevocation(ctx context.Context, fingerprint string) error {
	cfg := m.env.IdentityProxyConfigLookup()
	for _, r := range cfg.RevokedSessions {
		if r.Hash == fingerprint && (r.ExpiresAt == 0 || r.ExpiresAt > time.Now().Unix()) {
			return errInvalid
		}
	}
	if cfg.EffectiveRevocationMode() == "local" {
		if m.store.redis != nil {
			return errUnavailable
		}
		return nil
	}
	if cfg.EffectiveRevocationMode() != "shared" || m.store.redis == nil || m.env.IdentityProxyRevocations == nil || cfg.RevocationEpoch == "" {
		return errUnavailable
	}
	call, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	backend := m.env.IdentityProxyRevocations
	if _, err := backend.Prepare(call, cfg.RevocationEpoch, false); err != nil {
		return errUnavailable
	}
	if backend.Sync(call, cfg.RevokedSessions) != nil {
		return errUnavailable
	}
	revoked, err := backend.IsRevoked(call, fingerprint)
	if err != nil {
		return errUnavailable
	}
	if revoked {
		return errInvalid
	}
	return nil
}
func hasMapping(cfg config.IdentityProxyConfig, groups []string) bool {
	for _, m := range cfg.GroupMappings {
		for _, g := range groups {
			if m.Group == g {
				return true
			}
		}
	}
	return false
}
func assurance(amr []string, acr, required, allowed string) bool {
	for _, want := range split(required) {
		found := false
		for _, have := range amr {
			if have == want {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if allowed != "" {
		for _, want := range split(allowed) {
			if acr == want {
				return true
			}
		}
		return false
	}
	return true
}
func split(raw string) []string {
	var out []string
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
func random() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
func hash(raw string) string { h := sha256.Sum256([]byte(raw)); return hex.EncodeToString(h[:]) }

// IsUnavailable permits handlers to distinguish an outage from an invalid login
// without returning provider errors, token response bodies, or assertions.
func IsUnavailable(err error) bool { return errors.Is(err, errUnavailable) }

func identityPolicyHash(env *config.EnvConfig) string {
	return config.EntryKeyHash(env.NativeOIDCIssuer + "\x00" + env.NativeOIDCClientID + "\x00" + env.NativeOIDCRedirectURL + "\x00" + env.NativeOIDCRequiredAMR + "\x00" + env.NativeOIDCAllowedACR)
}
func (m *Manager) unchanged() bool {
	if m.policy != identityPolicyHash(m.env) {
		return false
	}
	cfg := m.env.IdentityProxyConfigLookup()
	return cfg.RevocationEpoch == m.revocationEpoch && ((m.store.redis == nil && cfg.EffectiveRevocationMode() == "local") || (m.store.redis != nil && cfg.EffectiveRevocationMode() == "shared"))
}
