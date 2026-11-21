package types

import (
	"encoding/json"
	"github.com/tidwall/gjson"
	"testing"
)

func TestGeminiNullableSchemaUsesLosslessField(t *testing.T) {
	for _, schema := range []string{
		`{"type":"object","properties":{"value":{"anyOf":[{"type":"string"},{"type":"null"}]}},"additionalProperties":false,"required":null}`,
		`{"type":"object","properties":{"value":{"type":["string","null"]}}}`,
		`{"type":"object","properties":{"items":{"type":"array","items":{"anyOf":[{"type":"null"},{"type":"object","properties":{"nested":{"anyOf":[{"type":"number"},{"type":"null"}]}}}]}}}}`,
	} {
		var decl GeminiFunctionDeclaration
		if err := json.Unmarshal([]byte(`{"name":"tool","parameters":`+schema+`}`), &decl); err != nil {
			t.Fatal(err)
		}
		out, err := json.Marshal(decl)
		if err != nil {
			t.Fatal(err)
		}
		if !gjson.GetBytes(out, "parametersJsonSchema").IsObject() || gjson.GetBytes(out, "parameters").Exists() {
			t.Fatalf("wrong wire field: %s", out)
		}
		if gjson.GetBytes([]byte(schema), "additionalProperties").Exists() && gjson.GetBytes(out, "parametersJsonSchema.additionalProperties").Raw != "false" {
			t.Fatalf("lost constraint %s", out)
		}
		if gjson.GetBytes(out, "parametersJsonSchema.required").Raw == "null" {
			t.Fatalf("invalid required:null retained %s", out)
		}
		if !GeminiSchemaRequiresJSONSchema(decl.Parameters) {
			t.Fatalf("union lost %s", out)
		}
	}
}
