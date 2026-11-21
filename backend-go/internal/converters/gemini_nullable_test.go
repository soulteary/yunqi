package converters

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/tidwall/gjson"
	"testing"
)

func TestResponsesGeminiNullableToolWire(t *testing.T) {
	var req types.ResponsesRequest
	raw := []byte(`{"model":"m","input":"hi","tools":[{"type":"function","name":"mcp","parameters":{"type":"object","properties":{"value":{"anyOf":[{"type":"null"},{"type":"string"}]}},"additionalProperties":false,"required":null}}]}`)
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	converted, err := ResponsesToGeminiRequest(&session.Session{}, &req, "m")
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(converted)
	if err != nil {
		t.Fatal(err)
	}
	schema := gjson.GetBytes(body, "tools.0.functionDeclarations.0.parametersJsonSchema")
	if !schema.IsObject() || schema.Get("properties.value.anyOf.0.type").String() != "null" || schema.Get("additionalProperties").Raw != "false" {
		t.Fatalf("lossy tool schema: %s", body)
	}
	if schema.Get("required").Raw == "null" {
		t.Fatalf("invalid required null: %s", body)
	}
}
