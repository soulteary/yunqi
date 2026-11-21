package handlers

import (
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestAccessPortalUsesNonceAndNoPersistentCredentialStorage(t *testing.T) {
	app := httpbridge.NewApp()
	app.Get("/access", AccessPortal(&config.EnvConfig{EnableWebUI: true}))
	w := httptest.NewRecorder()
	httpbridge.Handler(app).ServeHTTP(w, httptest.NewRequest("GET", "/access", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("portal response %d", w.Code)
	}
	nonce := regexp.MustCompile(`nonce="([A-Za-z0-9_-]+)"`).FindStringSubmatch(w.Body.String())
	if len(nonce) != 2 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "'nonce-"+nonce[1]+"'") || (strings.Contains(w.Body.String(), "__NONCE__") || strings.Contains(w.Body.String(), "__IDENTITY_PROXY_ENABLED__")) {
		t.Fatal("portal CSP does not bind its scripts")
	}
	for _, unsafe := range []string{"innerHTML", "localStorage", "sessionStorage", "?key=", "?api_key="} {
		if strings.Contains(w.Body.String(), unsafe) {
			t.Fatalf("unsafe portal credential/rendering API %s", unsafe)
		}
	}
	app = httpbridge.NewApp()
	app.Get("/access", AccessPortal(&config.EnvConfig{}))
	w = httptest.NewRecorder()
	httpbridge.Handler(app).ServeHTTP(w, httptest.NewRequest("GET", "/access", nil))
	if w.Code != 404 {
		t.Fatal("disabled web UI exposed access portal")
	}
}
