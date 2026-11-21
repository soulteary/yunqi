package converters

import (
	"encoding/json"
	"fmt"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ClaudeStructuredOutput converts only representable output modes. Schema bytes
// remain intact, including property order and additionalProperties constraints.
func ClaudeStructuredOutput(raw []byte) (json.RawMessage, error) {
	root := gjson.ParseBytes(raw)
	format := root.Get("text.format")
	chat := false
	if !format.Exists() && format.Raw != "null" {
		format = root.Get("response_format")
		chat = true
	}
	if !format.Exists() && format.Raw != "null" {
		return nil, nil
	}
	if !format.IsObject() {
		return nil, fmt.Errorf("structured output format must be an object")
	}
	switch format.Get("type").String() {
	case "text":
		return nil, nil
	case "json_schema":
		options := format
		if chat {
			options = format.Get("json_schema")
			if !options.IsObject() {
				return nil, fmt.Errorf("json_schema must be an object")
			}
		}
		if strict := options.Get("strict"); strict.Exists() && strict.Type != gjson.True && strict.Type != gjson.False {
			return nil, fmt.Errorf("structured output strict must be a boolean")
		}
		if name := options.Get("name"); name.Exists() && name.Type != gjson.String {
			return nil, fmt.Errorf("structured output name must be a string")
		}
		schema := format.Get("schema")
		if chat {
			schema = format.Get("json_schema.schema")
		}
		if !schema.IsObject() {
			return nil, fmt.Errorf("structured output json_schema requires an object schema")
		}
		out := `{"type":"json_schema"}`
		out, _ = sjson.SetRaw(out, "schema", schema.Raw)
		return json.RawMessage(out), nil
	default:
		return nil, fmt.Errorf("structured output mode %q cannot be represented by Claude", format.Get("type").String())
	}
}
