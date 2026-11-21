package converters

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/tidwall/gjson"
	"testing"
)

func TestStructuredOutputSchemaRoundTrip(t *testing.T) {
	schema := `{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"integer"}},"required":["z","a"],"additionalProperties":false}`
	chat := []byte(`{"model":"m","messages":[],"reasoning_effort":"high","response_format":{"type":"json_schema","json_schema":{"name":"result","strict":true,"schema":` + schema + `}}}`)
	raw := ConvertChatRequestToResponsesRequest(chat)
	if got := gjson.GetBytes(raw, "text.format.schema").Raw; got != schema {
		t.Fatalf("schema %s", got)
	}
	if got := gjson.GetBytes(raw, "reasoning.effort").String(); got != "high" {
		t.Fatalf("effort %s", got)
	}
	round := ConvertResponsesToOpenAIChatRequest("m", raw, false)
	if got := gjson.GetBytes(round, "response_format.json_schema.schema").Raw; got != schema {
		t.Fatalf("schema %s", got)
	}
	var req types.ResponsesRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	converted, err := (&ClaudeConverter{}).ToProviderRequest(&session.Session{}, &req)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(converted)
	if err != nil {
		t.Fatal(err)
	}
	if got := gjson.GetBytes(encoded, "output_config.format.schema").Raw; got != schema {
		t.Fatalf("Claude schema %s", got)
	}
}

func TestClaudeStructuredOutputRejectsUnsupported(t *testing.T) {
	for _, raw := range []string{`{"text":{"format":null}}`, `{"text":{"format":{"type":"json_object"}}}`, `{"text":{"format":{"type":"json_schema","schema":[]}}}`, `{"response_format":{"type":"json_schema"}}`} {
		if _, err := ClaudeStructuredOutput([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{}`, `{"text":{"format":{"type":"text"}}}`} {
		if _, err := ClaudeStructuredOutput([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
}
