package images

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestImagesSuccessCapturesDeliveredDimensionsWithoutChangingWire(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(recorder)
	const response = `{"data":[{"url":"https://example.test/first"},{"b64_json":"encoded-image"}]}`
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}
	usage, err := handleSuccess(c, resp, &config.EnvConfig{}, time.Now(), false, common.StreamPreflightTimeouts{}, &config.UpstreamConfig{}, []byte(`{"size":"1024x1024","quality":"high"}`), "application/json")
	if err != nil || usage == nil || len(usage.ImageUnits) != 1 || usage.ImageUnits[0].Count != 2 || usage.ImageUnits[0].Size != "1024x1024" || usage.ImageUnits[0].Quality != "high" {
		t.Fatalf("metering=%+v err=%v", usage, err)
	}
	if recorder.Body.String() != response {
		t.Fatal("internal image metering altered client response")
	}
	wire, _ := json.Marshal(usage)
	if strings.Contains(string(wire), "ImageUnits") || strings.Contains(string(wire), "1024x1024") {
		t.Fatal("internal price dimensions leaked onto usage wire")
	}
	if usage.Provenance == nil || strings.Contains(string(usage.Provenance.RawUsage), "example.test") {
		t.Fatal("metering provenance omitted or contains media URL")
	}
}
