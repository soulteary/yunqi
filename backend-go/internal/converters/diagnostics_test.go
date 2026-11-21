package converters

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestConversionDiagnosticsAreMachineReadableAndSecretFree(t *testing.T) {
	source := []byte(`{"prompt_cache_key":"secret-cache-id","prompt_cache_options":{"prewarm":false},"prompt_cache_retention":null}`)
	report, err := AnalyzeConversion(source, []byte(`{"messages":[]}`), "responses", "messages")
	if err != nil {
		t.Fatal(err)
	}
	if report.Quality != "lossy" || len(report.Diagnostics) != 3 {
		t.Fatalf("report %#v", report)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "secret-cache-id") || strings.Contains(string(encoded), "prewarm") {
		t.Fatalf("values leaked: %s", encoded)
	}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Action != "dropped" || diagnostic.Code != "unsupported_cache_hint" {
			t.Fatalf("diagnostic %#v", diagnostic)
		}
	}
}

func TestCriticalConversionLossIsRejected(t *testing.T) {
	for _, source := range []string{
		`{"text":{"format":{"type":"json_schema","schema":{"type":"object","additionalProperties":false}}}}`,
		`{"input":[{"type":"function_call_output","call_id":"a","output":[{"type":"input_image","image_url":"data:image/png;base64,c2VjcmV0"}]}]}`,
	} {
		report, err := AnalyzeConversion([]byte(source), []byte(`{"messages":[]}`), "responses", "chat")
		var loss *ConversionLossError
		if !errors.As(err, &loss) || report.Diagnostics[0].Action != "rejected" {
			t.Fatalf("silent critical loss: %#v %v", report, err)
		}
	}
}

func TestMappedSchemaMediaAndCacheArePreserved(t *testing.T) {
	source := []byte(`{"text":{"format":{"type":"json_schema","schema":{"type":"object","additionalProperties":false}}},"input":[{"type":"input_image","image_url":"data:image/png;base64,YQ=="}],"prompt_cache_options":{"prewarm":false},"prompt_cache_retention":null}`)
	target := []byte(`{"response_format":{"type":"json_schema","json_schema":{"schema":{"type":"object","additionalProperties":false}}},"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,YQ=="}}]}],"prompt_cache_options":{"prewarm":false},"prompt_cache_retention":null}`)
	report, err := AnalyzeConversion(source, target, "responses", "chat")
	if err != nil || report.Quality != "exact" || len(report.Diagnostics) != 4 {
		t.Fatalf("report %#v err=%v", report, err)
	}
}
