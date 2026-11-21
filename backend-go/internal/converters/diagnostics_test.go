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
	if err != nil || report.Quality != "partial" || len(report.Diagnostics) != 4 {
		t.Fatalf("report %#v err=%v", report, err)
	}
}

func TestConversionReportNeverClaimsFullExactCoverage(t *testing.T) {
	report, err := AnalyzeConversion([]byte(`{"arbitrary_extension":true}`), []byte(`{}`), "chat", "messages")
	if err != nil || report.Quality != "partial" || len(report.Coverage) == 0 {
		t.Fatalf("misleading coverage %#v %v", report, err)
	}
	for _, body := range []string{"not JSON", "null", "[]"} {
		report, err := AnalyzeConversion([]byte(body), []byte(`{}`), "chat", "messages")
		if err != nil || report.Quality != "unknown" || len(report.Diagnostics) == 0 || report.Diagnostics[0].Action != "unverified" {
			t.Fatalf("invalid request claimed verified %#v %v", report, err)
		}
	}
}

func TestToolSchemaConstraintAndRepresentationDiagnostics(t *testing.T) {
	source := []byte(`{"tools":[{"type":"function","name":"sensitive_tool_name","parameters":{"type":"object","additionalProperties":false,"properties":{"value":{"type":"string"}}}}]}`)
	target := []byte(`{"tools":[{"functionDeclarations":[{"name":"sensitive_tool_name","parameters":{"type":"object","properties":{"value":{"type":"string"}}}}]}]}`)
	report, err := AnalyzeConversion(source, target, "responses", "gemini")
	if err != nil || report.Quality != "lossy" || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "tool_schema_constraints_changed" {
		t.Fatalf("tool constraint loss hidden %#v %v", report, err)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "sensitive_tool_name") {
		t.Fatal("tool value leaked to report")
	}
	source = []byte(`{"tools":[{"name":"tool","input_schema":{"type":"object","properties":{"title":{"type":"string","const":"x","description":"hint"}}}}]}`)
	target = []byte(`{"tools":[{"functionDeclarations":[{"name":"tool","parameters":{"type":"object","properties":{"title":{"type":"string","enum":["x"]}}}}]}]}`)
	report, err = AnalyzeConversion(source, target, "messages", "gemini")
	if err != nil || report.Quality != "partial" || report.Diagnostics[0].Action != "converted" {
		t.Fatalf("equivalent representation mislabeled %#v %v", report, err)
	}
}
