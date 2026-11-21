package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/redaction"
)

func bodyLogAPIFixture(t *testing.T, role string, scoped bool) (*config.ConfigManager, *config.EnvConfig, string, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	cm, err := config.NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cm.UpdateBodyLogging("test:init", "", config.BodyLoggingSettings{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cm.UpdateBodyLogging("test:cleanup", "", config.BodyLoggingSettings{}); _ = cm.Close() })
	env := &config.EnvConfig{Env: "development", EnableRequestLogs: true, EnableResponseLogs: true, LogFile: "app.log", LogMaxAge: 7, LogMaxSize: 1}
	app := httpbridge.NewApp()
	app.Use(func(c fiber.Ctx) error {
		p := middleware.AdminPrincipal{ID: "actor:" + role, Role: role}
		if role == "" {
			p.ID = ""
		}
		if scoped {
			p.ChannelUIDs = []string{"one-channel"}
		}
		middleware.SetAdminPrincipal(c, p)
		return c.Next()
	})
	app.Get("/api/logging/body", GetBodyLogging(cm, env))
	app.Put("/api/logging/body", UpdateBodyLogging(cm, env))
	app.Get("/api/logging/body/export", DownloadBodyLogs(env, dir))
	return cm, env, dir, httpbridge.Handler(app)
}
func bodyLogRequest(handler http.Handler, method, path, body, revision string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if revision != "" {
		req.Header.Set("If-Match", revision)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}
func TestBodyLogConfigurationAndDownloadsRequireGlobalAdmin(t *testing.T) {
	for _, role := range []string{"", "viewer", "operator", "admin", "scoped-admin"} {
		t.Run(role, func(t *testing.T) {
			actual := role
			scoped := role == "scoped-admin"
			if scoped {
				actual = "admin"
			}
			cm, _, dir, handler := bodyLogAPIFixture(t, actual, scoped)
			if err := os.WriteFile(filepath.Join(dir, "app.log"), []byte("diagnostic\n"), 0600); err != nil {
				t.Fatal(err)
			}
			before := cm.GetBodyLoggingState().Revision
			for _, request := range []struct{ method, path, body string }{{"GET", "/api/logging/body", ""}, {"PUT", "/api/logging/body", `{"requestBodies":true}`}, {"GET", "/api/logging/body/export", ""}} {
				response := bodyLogRequest(handler, request.method, request.path, request.body, "")
				expected := 403
				if role == "admin" {
					expected = 200
				}
				if response.Code != expected {
					t.Fatalf("%s %s status=%d body=%s", role, request.path, response.Code, response.Body.String())
				}
			}
			if role != "admin" && cm.GetBodyLoggingState().Revision != before {
				t.Fatal("unauthorized update changed policy")
			}
		})
	}
}
func TestBodyLogAPIPatchRevisionAuditAndEffectiveFeedback(t *testing.T) {
	cm, env, _, handler := bodyLogAPIFixture(t, "admin", false)
	before := cm.GetBodyLoggingState().Revision
	response := bodyLogRequest(handler, "PUT", "/api/logging/body", `{"requestBodies":true}`, before)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if !cm.GetBodyLoggingState().Runtime.RequestBodies || cm.GetBodyLoggingState().Audit[0].ActorID != "actor:admin" {
		t.Fatal("missing applied setting or actor audit")
	}
	response = bodyLogRequest(handler, "PUT", "/api/logging/body", `{"responseBodies":true}`, before)
	if response.Code != 409 || cm.GetBodyLoggingState().Runtime.ResponseBodies {
		t.Fatal("stale save changed policy")
	}
	response = bodyLogRequest(handler, "PUT", "/api/logging/body", `{"maxBytes":1024}`, "")
	if response.Code != 200 || !cm.GetBodyLoggingState().Runtime.RequestBodies || cm.GetBodyLoggingState().Runtime.MaxBytes != 1024 {
		t.Fatal("omitted body flag was cleared")
	}
	for _, body := range []string{`{}`, `{"requestBodies":null}`, `{"unknown":true}`, `{"maxBytes":1}`, `{"requestBodies":"yes"}`} {
		response = bodyLogRequest(handler, "PUT", "/api/logging/body", body, "")
		if response.Code != 400 {
			t.Fatalf("invalid update %s status=%d", body, response.Code)
		}
	}
	env.Env = "production"
	response = bodyLogRequest(handler, "GET", "/api/logging/body", "", "")
	var result struct {
		Effective struct {
			RequestBodies bool `json:"requestBodies"`
		} `json:"effective"`
		Configured    config.BodyLoggingSettings `json:"configured"`
		RetentionDays int                        `json:"retentionDays"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Effective.RequestBodies || !result.Configured.RequestBodies || result.RetentionDays != 7 {
		t.Fatal("save feedback hid an inactive development logging gate")
	}
}
func TestBodyLogSaveFailureDoesNotEnableContent(t *testing.T) {
	cm, _, dir, handler := bodyLogAPIFixture(t, "admin", false)
	path := filepath.Join(dir, "config.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	response := bodyLogRequest(handler, "PUT", "/api/logging/body", `{"responseBodies":true}`, "")
	if response.Code != 500 || cm.GetBodyLoggingState().Runtime.ResponseBodies || !strings.Contains(response.Body.String(), `"persisted":false`) {
		t.Fatalf("failed save looked successful: %d %s", response.Code, response.Body.String())
	}
}
func TestBodyLogDownloadRedactsAndRejectsSymlinks(t *testing.T) {
	_, _, dir, handler := bodyLogAPIFixture(t, "admin", false)
	const secret = "opaque!download-credential=secret"
	redaction.Configure([]string{secret})
	defer redaction.Configure(nil)
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte(secret+"\npartial "+secret), 0600); err != nil {
		t.Fatal(err)
	}
	response := bodyLogRequest(handler, "GET", "/api/logging/body/export", "", "")
	if response.Code != 200 || strings.Contains(response.Body.String(), secret) || !strings.Contains(response.Body.String(), redaction.Mask) || strings.Contains(response.Body.String(), "partial") {
		t.Fatalf("unsafe download: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("body download is cacheable")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "config.json"), path); err != nil {
		t.Fatal(err)
	}
	if response := bodyLogRequest(handler, "GET", "/api/logging/body/export", "", ""); response.Code != 404 {
		t.Fatal("download followed a symlink")
	}
}
func TestBodyLogDownloadOnlyReturnsBoundedCompleteLines(t *testing.T) {
	data, err := readCompleteLogLines(strings.NewReader("safe\n"+strings.Repeat("x", 25)+"\npartial secret"), 16)
	if err != nil || string(data) != "safe\n" {
		t.Fatalf("partial/oversized line leaked: %q err=%v", data, err)
	}
	_, err = readCompleteLogLines(io.MultiReader(strings.NewReader("safe\n"), failedLogReader{}), 16)
	if err == nil {
		t.Fatal("read failure was hidden")
	}
}

type failedLogReader struct{}

func (failedLogReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
