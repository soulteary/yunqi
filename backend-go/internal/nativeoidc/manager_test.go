package nativeoidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpclient"
	"golang.org/x/oauth2"
)

type fakeIDP struct {
	server           *httptest.Server
	nonce, challenge string
	claims           map[string]interface{}
	key              *rsa.PrivateKey
	calls            int
	tokenEndpoint    string
	keysEndpoint     string
}

func fakeProvider(t *testing.T) (*Manager, *fakeIDP, *config.IdentityProxyConfig) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIDP{key: key}
	idp.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"issuer": idp.server.URL, "authorization_endpoint": idp.server.URL + "/authorize", "token_endpoint": idp.tokenURL(), "jwks_uri": idp.keysURL(), "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
		case "/redirect-token":
			http.Redirect(w, r, idp.server.URL+"/token", http.StatusTemporaryRedirect)
		case "/token":
			idp.calls++
			if r.ParseForm() != nil || oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != idp.challenge {
				http.Error(w, "bad verifier", 400)
				return
			}
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.key}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
			if err != nil {
				t.Error(err)
				return
			}
			raw, _ := json.Marshal(idp.claims)
			signed, _ := signer.Sign(raw)
			token, _ := signed.CompactSerialize()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "synthetic-access-token", "token_type": "Bearer", "expires_in": 3600, "id_token": token})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(idp.server.Close)
	cfg := &config.IdentityProxyConfig{GroupMappings: []config.IdentityGroupMapping{{Group: "ops", Role: "operator", ChannelUIDs: []string{"a"}}}}
	env := &config.EnvConfig{NativeOIDCIssuer: idp.server.URL, NativeOIDCClientID: "test-client", NativeOIDCRedirectURL: "https://console.example/auth/oidc/callback", NativeOIDCRequiredAMR: "mfa", NativeOIDCAllowedACR: "urn:strong", IdentityProxyConfigLookup: func() config.IdentityProxyConfig { return *cfg }}
	m, err := New(env)
	if err != nil {
		t.Fatal(err)
	}
	redirectPolicy := m.client.CheckRedirect
	m.client = idp.server.Client()
	m.client.CheckRedirect = redirectPolicy
	t.Cleanup(func() { _ = m.Close() })
	return m, idp, cfg
}
func startFlow(t *testing.T, m *Manager, idp *fakeIDP) (state, browser string) {
	t.Helper()
	raw, browser, err := m.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(raw)
	if u.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("missing PKCE")
	}
	idp.challenge = u.Query().Get("code_challenge")
	idp.nonce = u.Query().Get("nonce")
	idp.claims = map[string]interface{}{"iss": idp.server.URL, "aud": "test-client", "sub": "private-subject", "nonce": idp.nonce, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "amr": []string{"pwd", "mfa"}, "acr": "urn:strong", "groups": []string{"ops"}}
	return u.Query().Get("state"), browser
}
func TestOIDCCodeFlowAndRevocation(t *testing.T) {
	m, idp, cfg := fakeProvider(t)
	state, browser := startFlow(t, m, idp)
	cookie, err := m.Complete(context.Background(), state, "synthetic-code", browser)
	if err != nil {
		t.Fatal(err)
	}
	session, err := m.Lookup(context.Background(), cookie)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cookie, "private-subject") || session.SubjectID == "private-subject" || len(session.Groups) != 1 || session.Groups[0] != "ops" {
		t.Fatal("unsafe or missing session claims")
	}
	if _, err = m.Complete(context.Background(), state, "synthetic-code", browser); err == nil || idp.calls != 1 {
		t.Fatal("authorization flow replayed")
	}
	cfg.RevokedSessions = []config.IdentitySessionRevocation{{Hash: session.Fingerprint, ExpiresAt: session.ExpiresAt}}
	if _, err = m.Lookup(context.Background(), cookie); err == nil {
		t.Fatal("revoked native session authenticated")
	}
}
func TestOIDCRejectsInvalidAssertions(t *testing.T) {
	for _, name := range []string{"nonce", "issuer", "audience", "expired", "amr", "acr", "azp", "groups", "browser", "state", "signature", "future-iat"} {
		t.Run(name, func(t *testing.T) {
			m, idp, _ := fakeProvider(t)
			state, browser := startFlow(t, m, idp)
			switch name {
			case "signature":
				idp.key, _ = rsa.GenerateKey(rand.Reader, 2048)
			case "future-iat":
				idp.claims["iat"] = time.Now().Add(time.Hour).Unix()
			case "nonce":
				idp.claims["nonce"] = "other"
			case "issuer":
				idp.claims["iss"] = "https://evil.example"
			case "audience":
				idp.claims["aud"] = "other-client"
			case "expired":
				idp.claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "amr":
				idp.claims["amr"] = []string{"pwd"}
			case "acr":
				idp.claims["acr"] = "urn:weak"
			case "azp":
				idp.claims["aud"] = []string{"test-client", "other"}
				idp.claims["azp"] = "other"
			case "groups":
				idp.claims["groups"] = []string{"outsider"}
			case "browser":
				browser = strings.Repeat("a", 43)
			case "state":
				state = strings.Repeat("b", 43)
			}
			if _, err := m.Complete(context.Background(), state, "code", browser); err == nil {
				t.Fatal("accepted invalid assertion")
			}
			if (name == "state" || name == "browser") && idp.calls != 0 {
				t.Fatal("unbound flow exchanged code")
			}
		})
	}
}
func TestOIDCRequiresTLSAndAssurance(t *testing.T) {
	m, _, _ := fakeProvider(t)
	env := *m.env
	for _, change := range []func(){func() { env.NativeOIDCIssuer = "http://issuer.example" }, func() { env.NativeOIDCRedirectURL = "http://console.example/auth/oidc/callback" }, func() { env.NativeOIDCRequiredAMR = ""; env.NativeOIDCAllowedACR = "" }} {
		env = *m.env
		change()
		if _, err := New(&env); err == nil {
			t.Fatal("accepted insecure settings")
		}
	}
	if !assurance([]string{"fido"}, "urn:passkey", "fido", "urn:passkey") || assurance([]string{"mfa"}, "urn:passkey", "fido", "urn:passkey") {
		t.Fatal("passkey requirements not enforced")
	}
}
func TestOIDCLocalSessionCapacityEvictsOldestAndRestartInvalidates(t *testing.T) {
	m, idp, _ := fakeProvider(t)
	var cookies []string
	for i := 0; i < 21; i++ {
		state, browser := startFlow(t, m, idp)
		cookie, err := m.Complete(context.Background(), state, "code", browser)
		if err != nil {
			t.Fatal(err)
		}
		cookies = append(cookies, cookie)
	}
	if _, err := m.Lookup(context.Background(), cookies[0]); err == nil {
		t.Fatal("oldest session survived cap")
	}
	if _, err := m.Lookup(context.Background(), cookies[20]); err != nil {
		t.Fatal("new login blocked at cap", err)
	}
	fresh, err := New(m.env)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if _, err = fresh.Lookup(context.Background(), cookies[20]); err == nil {
		t.Fatal("local session resurrected after restart")
	}
}
func TestOIDCOutboundPolicyAppliesToDiscovery(t *testing.T) {
	m, _, _ := fakeProvider(t)
	m.client = httpclient.GetManager().GetStandardClient(2*time.Second, false)
	t.Setenv("YUNQI_OUTBOUND_ALLOWED_HOSTS", "allowed.invalid")
	if _, _, err := m.Begin(context.Background()); err == nil {
		t.Fatal("discovery bypassed outbound policy")
	}
}
func TestOIDCIssuanceLimitDoesNotEvictExistingSessions(t *testing.T) {
	s, _ := newStorage("", "")
	ctx := context.Background()
	expires := time.Now().Add(time.Hour).Unix()
	for i := 0; i < 100; i++ {
		if err := s.put(ctx, "session:"+string(rune(i+100)), "value", "subject", expires); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.put(ctx, "excess", "value", "subject", expires); err == nil {
		t.Fatal("issuance limit ignored")
	}
	if _, err := s.get(ctx, "session:"+string(rune(199)), "subject", false); err != nil {
		t.Fatal("rejected issuance evicted current session")
	}
}

func (f *fakeIDP) tokenURL() string {
	if f.tokenEndpoint != "" {
		return f.tokenEndpoint
	}
	return f.server.URL + "/token"
}
func (f *fakeIDP) keysURL() string {
	if f.keysEndpoint != "" {
		return f.keysEndpoint
	}
	return f.server.URL + "/keys"
}
func TestOIDCPolicyProtectsTokenAndJWKSAndRejectsTokenRedirect(t *testing.T) {
	for _, kind := range []string{"token", "keys", "redirect"} {
		t.Run(kind, func(t *testing.T) {
			m, idp, _ := fakeProvider(t)
			redirectPolicy := m.client.CheckRedirect
			transport := httpclient.GetManager().GetStandardClient(2*time.Second, true).Transport
			m.client = &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: redirectPolicy}
			t.Setenv("YUNQI_OUTBOUND_ALLOWED_HOSTS", "127.0.0.1")
			state, browser := startFlow(t, m, idp)
			switch kind {
			case "token":
				idp.tokenEndpoint = "https://blocked.invalid/token"
			case "keys":
				idp.keysEndpoint = "https://blocked.invalid/keys"
			case "redirect":
				idp.tokenEndpoint = idp.server.URL + "/redirect-token"
			}
			if _, err := m.Complete(context.Background(), state, "code", browser); err == nil {
				t.Fatal("restricted endpoint or token redirect accepted")
			}
		})
	}
}
func TestOIDCSessionLifetimeHonorsCurrentPolicy(t *testing.T) {
	m, idp, cfg := fakeProvider(t)
	cfg.MaxSessionSeconds = 60
	state, browser := startFlow(t, m, idp)
	cookie, err := m.Complete(context.Background(), state, "code", browser)
	if err != nil {
		t.Fatal(err)
	}
	active, err := m.Lookup(context.Background(), cookie)
	if err != nil || active.ExpiresAt-active.IssuedAt > 60 {
		t.Fatal("configured lifetime not applied", err)
	}
	subject, _, _ := strings.Cut(cookie, ".")
	raw, err := m.store.get(context.Background(), "session:"+hash(cookie), subject, false)
	if err != nil {
		t.Fatal(err)
	}
	var saved session
	_ = json.Unmarshal([]byte(raw), &saved)
	saved.IssuedAt = time.Now().Add(-2 * time.Hour).Unix()
	saved.ExpiresAt = time.Now().Add(time.Hour).Unix()
	_ = m.store.put(context.Background(), "session:"+hash(cookie), marshal(saved), subject, saved.ExpiresAt)
	if _, err = m.Lookup(context.Background(), cookie); err == nil {
		t.Fatal("shorter current policy did not expire old session")
	}
}
func TestOIDCAssurancePolicyChangeInvalidatesExistingSessions(t *testing.T) {
	m, idp, _ := fakeProvider(t)
	state, browser := startFlow(t, m, idp)
	cookie, err := m.Complete(context.Background(), state, "code", browser)
	if err != nil {
		t.Fatal(err)
	}
	oldPrefix := m.store.prefix
	m.env.NativeOIDCRequiredAMR = "fido"
	if _, err = m.Lookup(context.Background(), cookie); !IsUnavailable(err) {
		t.Fatal("live policy change accepted old session", err)
	}
	fresh, err := New(m.env)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if fresh.store.prefix == oldPrefix {
		t.Fatal("stronger policy reused session realm")
	}
	// Reusing the old storage data cannot make the old cookie valid in the new realm.
	fresh.store.local = m.store.local
	if _, err = fresh.Lookup(context.Background(), cookie); err == nil {
		t.Fatal("stronger policy accepted low-assurance session")
	}
}
