package vectors

import (
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelImportVectorBuilderUsesRawID(t *testing.T) {
	c, _ := httpbridge.TestContext(httptest.NewRecorder())
	httpbridge.SetRequest(c, httptest.NewRequest("POST", "/v1/embeddings", nil))
	up := &config.UpstreamConfig{ServiceType: "openai", ModelImport: &config.ModelImportState{Policy: config.ModelImportPolicy{Enabled: true}, Approved: []config.ImportedModel{{ID: "public", UpstreamID: "provider/raw"}}}}
	req, err := buildProviderRequest(c, up, "https://example.invalid", "fake", []byte(`{"model":"public","input":["hello"],"dimensions":256}`), "public")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(req.Body)
	if !strings.Contains(string(body), `"model":"provider/raw"`) || !strings.Contains(string(body), `"dimensions":256`) {
		t.Fatalf("bad provider body %s", body)
	}
	if _, err = buildProviderRequest(c, up, "https://example.invalid", "fake", []byte(`{"model":"other","input":"hello"}`), "other"); err == nil {
		t.Fatal("unapproved vector accepted")
	}
}
