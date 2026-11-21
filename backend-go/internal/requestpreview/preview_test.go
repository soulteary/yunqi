package requestpreview

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/config"
	"net/http"
	"strings"
	"testing"
)

func TestPreviewUsesLiveBuilderAndReportsSampleEffects(t *testing.T) {
	up := config.UpstreamConfig{ServiceType: "openai", BaseURL: "https://example.test", APIKeys: []string{"opaque-preview-secret"}, TextVerbosity: "high", CustomHeaders: map[string]string{"X-Custom-Identity": "draft-secret"}}
	result, err := Build(up, Input{Protocol: "chat", Model: "gpt-test", Body: json.RawMessage(`{"model":"gpt-test","messages":[{"role":"user","content":"hello draft-secret"}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), "opaque-preview-secret") || strings.Contains(string(data), "draft-secret") {
		t.Fatalf("preview leaked credentials: %s", data)
	}
	if result.Mode != "native_with_compatibility" || result.Method != http.MethodPost || !strings.HasSuffix(result.URL, "/v1/chat/completions") {
		t.Fatalf("wrong live wire: %+v", result)
	}
	if !strings.Contains(string(result.Body), `"verbosity":"high"`) {
		t.Fatalf("live builder option missing: %s", result.Body)
	}
	found := false
	for _, effect := range result.Effects {
		if effect.Field == "textVerbosity" && effect.Status == "applied_to_sample" {
			found = true
		}
	}
	if !found {
		t.Fatalf("actual option effect missing: %+v", result.Effects)
	}
	if up.CustomHeaders["X-Custom-Identity"] != "draft-secret" {
		t.Fatal("preview mutated saved configuration")
	}
}
func TestPreviewDraftLimitsAndOAuthNeverResolve(t *testing.T) {
	for _, input := range []Input{
		{Protocol: "chat", Body: json.RawMessage(`{}`), Draft: map[string]json.RawMessage{"apiKeys": json.RawMessage(`["no"]`)}},
		{Protocol: "chat", Body: json.RawMessage(`{}`), Draft: map[string]json.RawMessage{"fastMode": json.RawMessage(`"wrong"`)}},
		{Protocol: "chat", Body: json.RawMessage(strings.Repeat("x", MaxSampleBytes+1))},
	} {
		if _, err := Build(config.UpstreamConfig{ServiceType: "openai"}, input); err == nil {
			t.Fatal("unsafe draft accepted")
		}
	}
	if _, err := Build(config.UpstreamConfig{ServiceType: "copilot", BaseURL: "http://127.0.0.1:1"}, Input{Protocol: "chat", Body: json.RawMessage(`{}`)}); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("OAuth preview should refuse before resolving: %v", err)
	}
}
func TestGeminiPreviewReusesNativeCompatibilityAndMasksQueryKey(t *testing.T) {
	result, err := Build(config.UpstreamConfig{ServiceType: "gemini", BaseURL: "https://example.test", APIKeys: []string{"query-secret"}, StripThoughtSignature: true}, Input{Protocol: "gemini", Model: "gemini-test", Body: json.RawMessage(`{"contents":[{"role":"user","parts":[{"text":"hello","thoughtSignature":"remove-me"}]}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.URL, "query-secret") || strings.Contains(string(result.Body), "remove-me") {
		t.Fatalf("native compatibility/redaction not applied: %+v", result)
	}
}

func TestPreviewNullDraftResetsScalarWithoutSaving(t *testing.T) {
	up := config.UpstreamConfig{ServiceType: "openai", FastMode: true, TextVerbosity: "high"}
	draft, err := ApplyDraft(up, map[string]json.RawMessage{"fastMode": json.RawMessage(`null`), "textVerbosity": json.RawMessage(`null`)})
	if err != nil || draft.FastMode || draft.TextVerbosity != "" || !up.FastMode || up.TextVerbosity != "high" {
		t.Fatalf("null draft did not reset independent copy: %+v %v", draft, err)
	}
}
