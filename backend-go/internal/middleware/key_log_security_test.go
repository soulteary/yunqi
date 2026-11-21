package middleware

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestLegacyKeyPathsAreRedactedFromRequestAndAuthLogs(t *testing.T) {
	for _, kind := range []string{"messages", "chat", "responses", "gemini", "images", "vectors"} {
		for _, action := range []string{"", "/top", "/bottom"} {
			for _, authenticated := range []bool{false, true} {
				for _, secret := range []string{"DUMMY_SECRET_PLAIN", "DUMMY_SECRET/space +percent%"} {
					t.Run(kind+action+"/"+secret+"/"+map[bool]string{true: "authorized", false: "unauthorized"}[authenticated], func(t *testing.T) {
						var output bytes.Buffer
						old := log.Writer()
						log.SetOutput(&output)
						defer log.SetOutput(old)
						env := &config.EnvConfig{EnableWebUI: true, ProxyAccessKey: "DUMMY_ADMIN", LogLevel: "info", QuietPollingLogs: false}
						r := httpbridge.NewApp()
						r.Use(FilteredLogger(env))
						r.Use(WebAuthMiddleware(env, nil))
						method := http.MethodPost
						if action == "" {
							method = http.MethodDelete
						}
						path := "/api/" + kind + "/channels/0/keys/" + url.PathEscape(secret) + action
						r.Add([]string{method}, "/api/"+kind+"/channels/:id/keys/:apiKey"+action, func(c fiber.Ctx) error { httpbridge.JSON(c, 200, fiber.Map{"ok": true}); return nil })
						req := httptest.NewRequest(method, path, nil)
						if authenticated {
							req.Header.Set("x-api-key", "DUMMY_ADMIN")
						}
						w := httptest.NewRecorder()
						httpbridge.Handler(r).ServeHTTP(w, req)
						want := 401
						if authenticated {
							want = 200
							if strings.Contains(secret, "/") {
								want = 404
							}
						}
						if w.Code != want {
							t.Fatalf("status=%d, want=%d: %s", w.Code, want, w.Body.String())
						}
						got := output.String()
						if strings.Contains(got, secret) || strings.Contains(got, url.PathEscape(secret)) || strings.Contains(got, "DUMMY_SECRET") {
							t.Fatalf("key leaked: %s", got)
						}
						if !strings.Contains(got, "[REDACTED]") || !strings.Contains(got, "[HTTP]") || !strings.Contains(got, "[Auth-") {
							t.Fatalf("missing sanitized audit log: %s", got)
						}
					})
				}
			}
		}
	}
}

func TestSanitizeLogPathPreservesNonKeyPaths(t *testing.T) {
	for _, path := range []string{"/health", "/api/messages/channels/0", "/api/channels/ch_uid/keys/key_hash", "/api/messages/channels/0/keys"} {
		if got := sanitizeLogPath(path, http.MethodGet); got != path {
			t.Fatalf("%q => %q", path, got)
		}
	}
}

func TestQuietPollingStillRedactsLegacyMutationLogs(t *testing.T) {
	var output bytes.Buffer
	old := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(old)
	r := httpbridge.NewApp()
	r.Use(FilteredLogger(&config.EnvConfig{QuietPollingLogs: true}))
	r.Post("/api/messages/channels/:id/keys/:apiKey/top", func(c fiber.Ctx) error { return nil })
	w := httptest.NewRecorder()
	httpbridge.Handler(r).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/messages/channels/0/keys/DUMMY_QUIET_SECRET/top", nil))
	if got := output.String(); strings.Contains(got, "DUMMY_QUIET_SECRET") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("unsafe audit log: %s", got)
	}
}

func TestSanitizeLogPathRetainsBodyMutationActionsOnlyForPOST(t *testing.T) {
	for _, kind := range []string{"messages", "chat", "responses", "gemini", "images", "vectors"} {
		for _, action := range []string{"remove", "move-to-top", "move-to-bottom"} {
			path := "/api/" + kind + "/channels/0/keys/" + action
			if got := sanitizeLogPath(path, http.MethodPost); got != path {
				t.Fatalf("body operation changed: %s", got)
			}
			if got := sanitizeLogPath(path, http.MethodDelete); strings.Contains(got, action) || !strings.Contains(got, "[REDACTED]") {
				t.Fatalf("legacy key leaked: %s", got)
			}
		}
	}
}

// Fiber normalizes dot segments before routing. Log that same path so a raw
// alias of a legacy key endpoint cannot bypass the redaction matcher.
func TestNormalizedLegacyKeyPathsAreRedactedFromLogs(t *testing.T) {
	for _, kind := range []string{"messages", "chat", "responses", "gemini", "images", "vectors"} {
		for _, alias := range []string{"./", "extra/../", "%2e/", "extra/%2E%2E/"} {
			for _, secret := range []string{"DUMMY_NORMALIZED_SECRET", "DUMMY_NORMALIZED_SECRET/space +percent%"} {
				for _, authenticated := range []bool{false, true} {
					t.Run(kind+"/"+alias+"/"+secret+"/"+map[bool]string{true: "authorized", false: "unauthorized"}[authenticated], func(t *testing.T) {
						var output bytes.Buffer
						old := log.Writer()
						log.SetOutput(&output)
						defer log.SetOutput(old)
						env := &config.EnvConfig{EnableWebUI: true, ProxyAccessKey: "DUMMY_ADMIN", LogLevel: "info"}
						router := httpbridge.NewApp()
						router.Use(FilteredLogger(env))
						router.Use(WebAuthMiddleware(env, nil))
						reached := false
						router.Delete("/api/"+kind+"/channels/:id/keys/:apiKey", func(c fiber.Ctx) error {
							reached = true
							httpbridge.JSON(c, http.StatusOK, fiber.Map{"ok": true})
							return nil
						})
						path := "/api/" + kind + "/" + alias + "channels/0/keys/" + url.PathEscape(secret)
						req := httptest.NewRequest(http.MethodDelete, path, nil)
						if authenticated {
							req.Header.Set("x-api-key", "DUMMY_ADMIN")
						}
						w := httptest.NewRecorder()
						httpbridge.Handler(router).ServeHTTP(w, req)
						want := http.StatusUnauthorized
						if authenticated {
							want = http.StatusOK
							// Existing path-based endpoints cannot represent a key containing a
							// slash. They still must not disclose it in error or request logs.
							if strings.Contains(secret, "/") {
								want = http.StatusNotFound
							}
						}
						if w.Code != want {
							t.Fatalf("status=%d, want=%d: %s", w.Code, want, w.Body.String())
						}
						if reached != (want == http.StatusOK) {
							t.Fatalf("handler reached=%t for status=%d", reached, w.Code)
						}
						got := output.String()
						if strings.Contains(got, "DUMMY_NORMALIZED_SECRET") || strings.Contains(got, url.PathEscape(secret)) {
							t.Fatalf("normalized key route leaked: %s", got)
						}
						if !strings.Contains(got, "[REDACTED]") || !strings.Contains(got, "[HTTP]") || !strings.Contains(got, "[Auth-") {
							t.Fatalf("missing sanitized audit log: %s", got)
						}
					})
				}
			}
		}
	}
}

func TestNormalizedBodyMutationLogsNeverIncludeDiscardedPathSecrets(t *testing.T) {
	for _, kind := range []string{"messages", "chat", "responses", "gemini", "images", "vectors"} {
		for _, authenticated := range []bool{false, true} {
			t.Run(kind+"/"+map[bool]string{true: "authorized", false: "unauthorized"}[authenticated], func(t *testing.T) {
				var output bytes.Buffer
				old := log.Writer()
				log.SetOutput(&output)
				defer log.SetOutput(old)
				env := &config.EnvConfig{EnableWebUI: true, ProxyAccessKey: "DUMMY_ADMIN", LogLevel: "info"}
				router := httpbridge.NewApp()
				router.Use(FilteredLogger(env))
				router.Use(WebAuthMiddleware(env, nil))
				router.Post("/api/"+kind+"/channels/:id/keys/remove", func(c fiber.Ctx) error {
					httpbridge.JSON(c, http.StatusOK, fiber.Map{"ok": true})
					return nil
				})
				path := "/api/" + kind + "/channels/0/keys/DUMMY_DISCARDED_SECRET/../remove"
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"apiKey":"DUMMY_BODY_SECRET"}`))
				req.Header.Set("Content-Type", "application/json")
				if authenticated {
					req.Header.Set("x-api-key", "DUMMY_ADMIN")
				}
				w := httptest.NewRecorder()
				httpbridge.Handler(router).ServeHTTP(w, req)
				want := http.StatusUnauthorized
				if authenticated {
					want = http.StatusOK
				}
				if w.Code != want {
					t.Fatalf("status=%d, want=%d: %s", w.Code, want, w.Body.String())
				}
				got := output.String()
				if strings.Contains(got, "DUMMY_DISCARDED_SECRET") || strings.Contains(got, "DUMMY_BODY_SECRET") {
					t.Fatalf("discarded raw path or body leaked: %s", got)
				}
				if !strings.Contains(got, "/api/"+kind+"/channels/0/keys/remove") {
					t.Fatalf("body mutation audit action missing: %s", got)
				}
			})
		}
	}
}
