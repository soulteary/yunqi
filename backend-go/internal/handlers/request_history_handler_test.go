package handlers

import (
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestRequestHistoryHTTPValidation(t *testing.T) {
	s, err := metrics.NewSQLiteStore(&metrics.SQLiteStoreConfig{DBPath: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	app := httpbridge.NewApp()
	app.Get("/history", GetRequestHistory(s))
	app.Get("/disabled", GetRequestHistory(nil))
	for _, tt := range []struct {
		path string
		code int
	}{{"/disabled", 503}, {"/history", 400}, {"/history?since=0&until=2&limit=201", 400}, {"/history?since=0&until=2&type=invalid", 400}, {"/history?since=0&until=2&cursor=bad", 400}, {"/history?since=0&until=2", 200}} {
		w := httptest.NewRecorder()
		httpbridge.Handler(app).ServeHTTP(w, httptest.NewRequest("GET", tt.path, nil))
		if w.Code != tt.code {
			t.Fatalf("%s: %d %s", tt.path, w.Code, w.Body.String())
		}
	}
}
