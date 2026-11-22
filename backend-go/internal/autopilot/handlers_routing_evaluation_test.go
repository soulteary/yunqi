package autopilot

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/routingeval"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutingEvaluationAccessAndSchemaBoundary(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	store, err := NewABTestStoreWithDB(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		role  string
		scope []string
		body  string
		want  int
	}{
		{"viewer", nil, `{}`, 403},
		{"operator", nil, `{}`, 403},
		{"admin", []string{"channel"}, `{}`, 403},
		{"admin", nil, `{"prompt":"must-not-be-retained"}`, 400},
	} {
		app := httpbridge.NewApp()
		group := app.Group("/api", func(c fiber.Ctx) error {
			middleware.SetAdminPrincipal(c, middleware.AdminPrincipal{ID: "test-admin", Role: test.role, ChannelUIDs: test.scope})
			return c.Next()
		})
		RegisterRoutingEvaluationRoutes(group, store)
		req := httptest.NewRequest(http.MethodPost, "/api/autopilot/evaluations", strings.NewReader(test.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(w, req)
		if w.Code != test.want {
			t.Fatalf("role=%s status=%d body=%s", test.role, w.Code, w.Body.String())
		}
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM routing_evaluations").Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid input persisted: count=%d err=%v", count, err)
	}
}

func TestRoutingEvaluationPersistsRealHTTPBodyAndExportsReplay(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ab, err := NewABTestStoreWithDB(db)
	if err != nil {
		t.Fatal(err)
	}
	app := httpbridge.NewApp()
	group := app.Group("/api", func(c fiber.Ctx) error {
		middleware.SetAdminPrincipal(c, middleware.AdminPrincipal{ID: "admin", Role: "admin"})
		return c.Next()
	})
	RegisterRoutingEvaluationRoutes(group, ab)
	quality, cost, latency, retries, success := 1.0, 0.01, 50.0, 0, true
	d := routingeval.Dataset{Version: 1, BaselineVersion: "baseline", CandidateVersion: "candidate", FixtureSHA256: strings.Repeat("a", 64), GraderVersion: "grader", EvidenceSource: "offline_paired"}
	for i := 0; i < 30; i++ {
		m := routingeval.Measurement{Quality: &quality, CostUSD: &cost, LatencyMS: &latency, Retries: &retries, Success: &success}
		d.Cases = append(d.Cases, routingeval.Case{ID: fmt.Sprintf("case-%d", i), Stratum: "coding", Baseline: m, Candidate: m})
	}
	body, _ := json.Marshal(d)
	req := httptest.NewRequest(http.MethodPost, "/api/autopilot/evaluations", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	httpbridge.Handler(app).ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("save status=%d body=%s", w.Code, w.Body.String())
	}
	var saved routingeval.Saved
	if err = json.Unmarshal(w.Body.Bytes(), &saved); err != nil || saved.Report.Decision != "eligible_for_manual_review" {
		t.Fatalf("saved=%s err=%v", w.Body.String(), err)
	}
	w = httptest.NewRecorder()
	httpbridge.Handler(app).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/autopilot/evaluations/"+saved.Report.ID+"/dataset", nil))
	if w.Code != 200 {
		t.Fatalf("export=%d %s", w.Code, w.Body.String())
	}
	recovered, err := routingeval.Decode(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	report, err := routingeval.Evaluate(recovered)
	if err != nil || report.ID != saved.Report.ID {
		t.Fatalf("not replayable: %v", err)
	}
	if ab.GetStats().TotalRecords != 0 {
		t.Fatal("evaluation polluted live shadow measurements")
	}
}
