package requestpreview

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/config"
	"strings"
	"testing"
)

func TestModelImportOfflinePreviewUsesActualProviderModel(t *testing.T) {
	up := config.UpstreamConfig{ServiceType: "openai", BaseURL: "https://example.invalid", ModelImport: &config.ModelImportState{Policy: config.ModelImportPolicy{Enabled: true}, Approved: []config.ImportedModel{{ID: "public", UpstreamID: "provider/raw"}}}}
	result, err := Build(up, Input{Protocol: "chat", Model: "public", Body: json.RawMessage(`{"model":"public","messages":[{"role":"user","content":"hello"}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.Body), `"model":"provider/raw"`) {
		t.Fatalf("preview differs from actual alias mapping: %s", result.Body)
	}
	if _, err = Build(up, Input{Protocol: "chat", Model: "unreviewed", Body: json.RawMessage(`{"model":"unreviewed","messages":[]}`)}); err == nil {
		t.Fatal("preview accepted an unapproved model")
	}
}

func TestModelImportGeminiPreviewUsesRawURI(t *testing.T) {
	up := config.UpstreamConfig{ServiceType: "gemini", BaseURL: "https://example.invalid", ModelImport: &config.ModelImportState{Policy: config.ModelImportPolicy{Enabled: true}, Approved: []config.ImportedModel{{ID: "public", UpstreamID: "gemini-raw"}}}}
	result, err := Build(up, Input{Protocol: "gemini", Model: "public", Body: json.RawMessage(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if !strings.Contains(string(encoded), "gemini-raw:generateContent") || strings.Contains(string(result.Body), `"model"`) {
		t.Fatalf("incorrect Gemini preview %s", encoded)
	}
}
