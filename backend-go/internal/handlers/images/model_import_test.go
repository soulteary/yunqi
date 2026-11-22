package images

import (
	"bytes"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelImportMultipartBuilderPreservesFilesAndUsesRawID(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", "public")
	file, err := writer.CreateFormFile("image", "image.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte{0, 1, 2, 255, 4})
	_ = writer.Close()
	c, _ := httpbridge.TestContext(httptest.NewRecorder())
	httpbridge.SetRequest(c, httptest.NewRequest("POST", "/v1/images/edits", nil))
	up := &config.UpstreamConfig{ServiceType: "openai", ModelImport: &config.ModelImportState{Policy: config.ModelImportPolicy{Enabled: true}, Approved: []config.ImportedModel{{ID: "public", UpstreamID: "provider/raw"}}}}
	req, err := buildOperationRequest(c, up, "https://example.invalid", "fake", body.Bytes(), "public", "edits", writer.FormDataContentType())
	if err != nil {
		t.Fatal(err)
	}
	if err = req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	defer req.MultipartForm.RemoveAll()
	if req.FormValue("model") != "provider/raw" {
		t.Fatalf("wrong model %q", req.FormValue("model"))
	}
	f, _, err := req.FormFile("image")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	raw, _ := io.ReadAll(f)
	if !bytes.Equal(raw, []byte{0, 1, 2, 255, 4}) {
		t.Fatalf("file corrupted %v", raw)
	}
	if !strings.HasSuffix(req.URL.Path, "/images/edits") {
		t.Fatalf("wrong endpoint %s", req.URL)
	}
}
