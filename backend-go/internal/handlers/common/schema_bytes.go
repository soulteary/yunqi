package common

import (
	"fmt"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Effort normalization may change surrounding objects, but schema property
// order is part of structured output behavior and must remain provider supplied.
func preserveOutboundSchemaBytes(source, rewritten []byte) []byte {
	paths := []string{"response_format.json_schema.schema", "text.format.schema", "output_config.format.schema", "generationConfig.responseJsonSchema", "generationConfig.responseSchema"}
	for i, tool := range gjson.GetBytes(source, "tools").Array() {
		prefix := fmt.Sprintf("tools.%d.", i)
		for _, field := range []string{"parameters", "input_schema", "function.parameters"} {
			paths = append(paths, prefix+field)
		}
		for j := range tool.Get("functionDeclarations").Array() {
			for _, field := range []string{"parameters", "parametersJsonSchema"} {
				paths = append(paths, fmt.Sprintf("%sfunctionDeclarations.%d.%s", prefix, j, field))
			}
		}
	}
	for _, path := range paths {
		raw := gjson.GetBytes(source, path)
		if raw.Exists() && gjson.GetBytes(rewritten, path).Exists() {
			if restored, err := sjson.SetRawBytes(rewritten, path, []byte(raw.Raw)); err == nil {
				rewritten = restored
			}
		}
	}
	return rewritten
}
