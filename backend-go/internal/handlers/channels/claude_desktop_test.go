package channels

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestUpdateDesktopTierSaveAndReadback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(config.Config{Upstream: []config.UpstreamConfig{{Name: "deepseek", BaseURL: "https://example.com", APIKeys: []string{"test"}, ServiceType: "claude"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cm, err := config.NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	uid := cm.GetConfig().Upstream[0].ChannelUID
	router := httpbridge.NewApp()
	RegisterRoutesForTest(router, &Handler{cm: cm})
	for _, body := range []string{
		`{"kind":"messages","claudeDesktopModelTiers":{"deepseek-v4-pro":"sonnet","deepseek-flash":"haiku"}}`,
		`{"kind":"messages","claudeDesktopModelTiers":{"deepseek-v4-pro":"","deepseek-flash":"haiku"}}`,
	} {
		req := httptest.NewRequest(http.MethodPut, "/channels/"+uid, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		httpbridge.Handler(router).ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("save failed: %d %s", recorder.Code, recorder.Body.String())
		}
		var receipt struct {
			Persisted bool `json:"persisted"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &receipt); err != nil {
			t.Fatal(err)
		}
		if !receipt.Persisted {
			t.Fatal("missing save receipt")
		}
		// Reopen the actual saved file, including its authoritative protocol members.
		saved, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		reopenPath := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(reopenPath, saved, 0600); err != nil {
			t.Fatal(err)
		}
		loaded, err := config.NewConfigManager(reopenPath, "")
		if err != nil {
			t.Fatal(err)
		}
		cfg := loaded.GetConfig()
		loaded.Close()
		tiers := config.ClaudeDesktopModelTiersForView(cfg.Upstream[0])
		var expected struct {
			Tiers map[string]string `json:"claudeDesktopModelTiers"`
		}
		if err := json.Unmarshal([]byte(body), &expected); err != nil {
			t.Fatal(err)
		}
		for id, want := range expected.Tiers {
			if got, exists := tiers[id]; !exists || got != want {
				t.Fatalf("readback %s = %q, want %q", id, tiers[id], want)
			}
		}
		if len(cfg.Upstream[0].ModelCapabilities) != 0 {
			t.Fatal("saving a tier changed model capability overrides")
		}
		view := common.BuildChannelView(cfg.Upstream[0], 0)
		if _, ok := view["claudeDesktopModelTiers"]; !ok {
			t.Fatal("tier missing from editor view")
		}
	}
	req := httptest.NewRequest(http.MethodPut, "/channels/"+uid, bytes.NewBufferString(`{"claudeDesktopModelTiers":{"deepseek-v4-pro":"invalid"}}`))
	req.Header.Set("Content-Type", "application/json")
	invalid := httptest.NewRecorder()
	httpbridge.Handler(router).ServeHTTP(invalid, req)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid tier status = %d", invalid.Code)
	}

}
