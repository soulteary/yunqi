package handlers

import (
	"encoding/base64"
	"encoding/json"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestManagementAuditQueryPermissionsCursorAndStableSnapshot(t *testing.T) {
	cm, err := config.NewConfigManager(filepath.Join(t.TempDir(), "config.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	for _, id := range []string{"one", "two", "three"} {
		if err = cm.RecordManagementRequest(config.ManagementAuditContext{OperationID: "op_" + id, ActorID: "admin:test", Action: "PUT channels", Resource: "path:hash"}, "completed", 200); err != nil {
			t.Fatal(err)
		}
	}
	router := httpbridge.NewApp()
	router.Use(func(c fiber.Ctx) error {
		role := httpbridge.GetHeader(c, "X-Test-Role")
		if role == "" {
			role = "admin"
		}
		p := middleware.AdminPrincipal{ID: "test", Role: role}
		if httpbridge.GetHeader(c, "X-Test-Scope") != "" {
			p.ChannelUIDs = []string{"one"}
		}
		middleware.SetAdminPrincipal(c, p)
		return c.Next()
	})
	RegisterManagementAuditRoutes(router, cm, nil)
	call := func(path, role, scope string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-Test-Role", role)
		req.Header.Set("X-Test-Scope", scope)
		rec := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(rec, req)
		return rec
	}
	for _, item := range []struct{ role, scope string }{{"viewer", ""}, {"operator", ""}, {"admin", "limited"}} {
		if response := call("/audit", item.role, item.scope); response.Code != 403 {
			t.Fatal("audit escaped global-admin boundary", response.Code)
		}
	}
	first := call("/audit?limit=2&actorId=admin:test", "", "")
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	var page struct {
		Entries  []config.ManagementAuditEvent `json:"entries"`
		Next     string                        `json:"nextCursor"`
		Snapshot uint64                        `json:"snapshotSequence"`
	}
	if json.Unmarshal(first.Body.Bytes(), &page) != nil || len(page.Entries) != 2 || page.Next == "" {
		t.Fatal("invalid first page")
	}
	nextCursor := page.Next
	snapshot := page.Snapshot
	_ = cm.RecordManagementRequest(config.ManagementAuditContext{OperationID: "op_late", ActorID: "admin:test"}, "completed", 200)
	second := call("/audit?limit=2&actorId=admin:test&cursor="+url.QueryEscape(nextCursor), "", "")
	if second.Code != 200 {
		t.Fatal(second.Code, second.Body.String())
	}
	if json.Unmarshal(second.Body.Bytes(), &page) != nil || len(page.Entries) != 1 || page.Snapshot != snapshot || page.Entries[0].OperationID == "op_late" {
		t.Fatal("new events entered frozen cursor")
	}
	other, err := config.NewConfigManager(filepath.Join(t.TempDir(), "replica.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	for _, id := range []string{"other-one", "other-two", "other-three"} {
		if err = other.RecordManagementRequest(config.ManagementAuditContext{OperationID: "op_" + id, ActorID: "admin:test"}, "completed", 200); err != nil {
			t.Fatal(err)
		}
	}
	replica := httpbridge.NewApp()
	replica.Use(func(c fiber.Ctx) error {
		middleware.SetAdminPrincipal(c, middleware.AdminPrincipal{ID: "admin:test", Role: "admin"})
		return c.Next()
	})
	RegisterManagementAuditRoutes(replica, other, nil)
	mismatch := httptest.NewRecorder()
	httpbridge.Handler(replica).ServeHTTP(mismatch, httptest.NewRequest("GET", "/audit?limit=2&actorId=admin:test&cursor="+url.QueryEscape(nextCursor), nil))
	if mismatch.Code != 400 {
		t.Fatal("cursor mixed different replica histories", mismatch.Code)
	}
	changed := call("/audit?limit=2&actorId=another&cursor="+url.QueryEscape(nextCursor), "", "")
	if changed.Code != 400 {
		t.Fatal("cursor accepted changed filters")
	}
	if response := call("/audit?limit=201", "", ""); response.Code != 400 {
		t.Fatal("unbounded query accepted")
	}
}
func TestManagementAuditQueryRelatesBodyLoggingDomainTransaction(t *testing.T) {
	cm, err := config.NewConfigManager(filepath.Join(t.TempDir(), "config.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	before := config.RuntimeBodyLogging()
	defer func() { _, _ = cm.UpdateBodyLogging("system:cleanup", "", before) }()
	settings := before
	settings.RequestBodies = !before.RequestBodies
	audit := config.ManagementAuditContext{OperationID: "op_body", ActorID: "admin:test", Action: "PUT logging", Resource: "config:body"}
	if _, err = cm.UpdateBodyLoggingAudited(audit, "admin:test", "", settings); err != nil {
		t.Fatal(err)
	}
	router := httpbridge.NewApp()
	router.Use(func(c fiber.Ctx) error {
		middleware.SetAdminPrincipal(c, middleware.AdminPrincipal{ID: "admin:test", Role: "admin"})
		return c.Next()
	})
	RegisterManagementAuditRoutes(router, cm, nil)
	response := httptest.NewRecorder()
	httpbridge.Handler(router).ServeHTTP(response, httptest.NewRequest("GET", "/audit?operationId=op_body", nil))
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var page struct {
		Entries []config.ManagementAuditEvent `json:"entries"`
		Related struct {
			Body []config.BodyLogAuditEvent `json:"bodyLogging"`
		} `json:"related"`
	}
	if json.Unmarshal(response.Body.Bytes(), &page) != nil || len(page.Entries) != 1 || len(page.Related.Body) != 1 || page.Related.Body[0].OperationID != "op_body" {
		t.Fatal("domain audit was not linked to config commit")
	}
}

func TestManagementAuditExpiredRetentionCursorIsExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	candidate := config.Config{ManagementAudit: &config.ManagementAuditState{NextSequence: 50, Events: []config.ManagementAuditEvent{{Sequence: 45, Outcome: "committed"}, {Sequence: 50, Outcome: "committed"}}}}
	data, _ := json.Marshal(candidate)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cm, err := config.NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	filters := map[string]string{"actorId": "", "action": "", "outcome": "", "operationId": ""}
	rawFilter, _ := json.Marshal(struct {
		Filters map[string]string
		Limit   int
	}{filters, 100})
	cursor := auditCursor{FirstRetained: 1, Before: 49, Snapshot: 50, Anchor: managementAuditAnchor(cm.ManagementAuditSnapshot(), 50), Filter: config.EntryKeyHash(string(rawFilter))}
	encoded, _ := json.Marshal(cursor)
	router := httpbridge.NewApp()
	router.Use(func(c fiber.Ctx) error {
		middleware.SetAdminPrincipal(c, middleware.AdminPrincipal{ID: "admin:test", Role: "admin"})
		return c.Next()
	})
	RegisterManagementAuditRoutes(router, cm, nil)
	response := httptest.NewRecorder()
	httpbridge.Handler(router).ServeHTTP(response, httptest.NewRequest("GET", "/audit?cursor="+base64.RawURLEncoding.EncodeToString(encoded), nil))
	if response.Code != 409 {
		t.Fatal("partially expired history was silently presented as a complete page", response.Code, response.Body.String())
	}
	legacy := map[string]interface{}{"before": 49, "snapshot": 50, "filter": cursor.Filter, "anchor": cursor.Anchor}
	encoded, _ = json.Marshal(legacy)
	response = httptest.NewRecorder()
	httpbridge.Handler(router).ServeHTTP(response, httptest.NewRequest("GET", "/audit?cursor="+base64.RawURLEncoding.EncodeToString(encoded), nil))
	if response.Code != 409 {
		t.Fatal("legacy cursor inherited current retention floor", response.Code)
	}
	cursor.Before, cursor.Snapshot = 10, 10
	encoded, _ = json.Marshal(cursor)
	response = httptest.NewRecorder()
	httpbridge.Handler(router).ServeHTTP(response, httptest.NewRequest("GET", "/audit?cursor="+base64.RawURLEncoding.EncodeToString(encoded), nil))
	if response.Code != 409 {
		t.Fatal("fully expired history was silently presented as empty", response.Code)
	}
}
