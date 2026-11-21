package middleware

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"net/http/httptest"
	"testing"
)

func TestPrincipalAdmissionIsolationAndRelease(t *testing.T) {
	env := &config.EnvConfig{ProxyAccessKey: "one", ExtraProxyAccessKeys: []string{"two"}, ProxyMaxConcurrency: 1}
	app := httpbridge.NewApp()
	app.Use(ProxyAdmissionMiddleware(env))
	entered, release := make(chan struct{}), make(chan struct{})
	app.Post("/v1/chat/completions", func(c fiber.Ctx) error {
		if GetPrincipalID(c) == "" {
			t.Error("missing principal")
		}
		if httpbridge.Request(c).Header.Get("Block") == "yes" {
			close(entered)
			<-release
		}
		httpbridge.Text(c, 200, "ok")
		return nil
	})
	invoke := func(key string, block bool) int {
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		req.Header.Set("x-api-key", key)
		if block {
			req.Header.Set("Block", "yes")
		}
		w := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(w, req)
		return w.Code
	}
	done := make(chan int, 1)
	go func() { done <- invoke("one", true) }()
	<-entered
	if got := invoke("one", false); got != 429 {
		t.Errorf("same principal: %d", got)
	}
	if got := invoke("two", false); got != 200 {
		t.Errorf("independent principal: %d", got)
	}
	if got := invoke("wrong", false); got != 401 {
		t.Errorf("invalid key: %d", got)
	}
	close(release)
	if got := <-done; got != 200 {
		t.Errorf("first request: %d", got)
	}
	if got := invoke("one", false); got != 200 {
		t.Errorf("lease not released: %d", got)
	}
}

func TestPrincipalIdentitySupportsRotationWithoutMaskCollisions(t *testing.T) {
	env := &config.EnvConfig{ProxyAccessKey: "old", ExtraProxyAccessKeys: []string{"new"}, ProxyPrincipalIDs: map[string]string{"old": "team", "new": "team"}}
	app := httpbridge.NewApp()
	app.Post("/v1/messages", ProxyAuthMiddleware(env), func(c fiber.Ctx) error {
		httpbridge.Text(c, 200, "%s", GetPrincipalID(c))
		return nil
	})
	for _, key := range []string{"old", "new"} {
		req := httptest.NewRequest("POST", "/v1/messages", nil)
		req.Header.Set("x-api-key", key)
		w := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(w, req)
		if w.Body.String() != "principal:team" {
			t.Fatalf("identity %s: %s", key, w.Body.String())
		}
	}
}

func TestPrincipalAdmissionSharedAcrossRouters(t *testing.T) {
	env := &config.EnvConfig{ProxyAccessKey: "key", ProxyMaxConcurrency: 1}
	entered, release := make(chan struct{}), make(chan struct{})
	invoke := func(block bool) int {
		app := httpbridge.NewApp()
		app.Use(ProxyAdmissionMiddleware(env))
		app.Post("/v1/responses", func(c fiber.Ctx) error {
			if block {
				close(entered)
				<-release
			}
			httpbridge.Text(c, 200, "ok")
			return nil
		})
		req := httptest.NewRequest("POST", "/v1/responses", nil)
		req.Header.Set("x-api-key", "key")
		w := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(w, req)
		return w.Code
	}
	done := make(chan int, 1)
	go func() { done <- invoke(true) }()
	<-entered
	if got := invoke(false); got != 429 {
		t.Errorf("second router bypassed shared gate: %d", got)
	}
	close(release)
	<-done
	if got := invoke(false); got != 200 {
		t.Errorf("slot not released: %d", got)
	}
}
