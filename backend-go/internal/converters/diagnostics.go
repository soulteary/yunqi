package converters

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// ConversionReport exposes field dispositions, never request values or media.
type ConversionReport struct {
	Steps       []ConversionStep       `json:"steps"`
	Quality     string                 `json:"quality"`
	Diagnostics []ConversionDiagnostic `json:"diagnostics"`
}
type ConversionStep struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type ConversionDiagnostic struct {
	Field  string `json:"field"`
	Action string `json:"action"`
	Code   string `json:"code"`
}

type ConversionLossError struct{ Report ConversionReport }

func (e *ConversionLossError) Error() string {
	if len(e.Report.Diagnostics) == 0 {
		return "protocol conversion rejected: unrepresentable field"
	}
	return fmt.Sprintf("protocol conversion rejected: %s cannot be represented without loss", e.Report.Diagnostics[len(e.Report.Diagnostics)-1].Field)
}

// AnalyzeConversion audits the final wire request, after all provider rewrites.
// Structured output and media loss are hard errors; optional vendor cache hints
// can be dropped across vendors only with a machine-readable diagnostic.
func AnalyzeConversion(source, target []byte, from, to string) (ConversionReport, error) {
	report := ConversionReport{Steps: []ConversionStep{{From: from, To: to}}, Quality: "exact", Diagnostics: []ConversionDiagnostic{}}
	var before, after map[string]interface{}
	if json.Unmarshal(source, &before) != nil || json.Unmarshal(target, &after) != nil {
		return report, nil
	}
	add := func(field, action, code string) {
		report.Diagnostics = append(report.Diagnostics, ConversionDiagnostic{field, action, code})
		if action != "preserved" {
			report.Quality = "lossy"
		}
	}
	format, hasFormat := conversionOutputFormat(before)
	if hasFormat {
		destination, hasDestination := conversionOutputFormat(after)
		if !hasDestination {
			add("structured_output", "rejected", "structured_output_mode_lost")
			return report, &ConversionLossError{report}
		}
		if kind, _ := format["type"].(string); kind == "json_schema" {
			schema, ok := format["schema"].(map[string]interface{})
			if !ok || !reflect.DeepEqual(schema, destination["schema"]) {
				add("structured_output.schema", "rejected", "schema_constraints_lost")
				return report, &ConversionLossError{report}
			}
		} else if format["type"] != destination["type"] {
			add("structured_output", "rejected", "structured_output_mode_changed")
			return report, &ConversionLossError{report}
		}
		add("structured_output", "preserved", "mapped")
	}
	originalMedia, convertedMedia := conversionMediaCount(before), conversionMediaCount(after)
	if originalMedia > 0 {
		if convertedMedia < originalMedia {
			add("media", "rejected", "media_content_lost")
			return report, &ConversionLossError{report}
		}
		add("media", "preserved", "mapped")
	}
	for _, field := range []string{"prompt_cache_key", "prompt_cache_options", "prompt_cache_retention"} {
		value, present := before[field]
		if !present {
			continue
		}
		converted, exists := after[field]
		if exists && reflect.DeepEqual(value, converted) {
			add(field, "preserved", "copied")
		} else {
			add(field, "dropped", "unsupported_cache_hint")
		}
	}
	return report, nil
}

func conversionOutputFormat(request map[string]interface{}) (map[string]interface{}, bool) {
	if text, ok := request["text"].(map[string]interface{}); ok {
		if format, ok := text["format"].(map[string]interface{}); ok {
			return format, true
		}
	}
	if format, ok := request["response_format"].(map[string]interface{}); ok {
		if format["type"] == "json_schema" {
			if schema, ok := format["json_schema"].(map[string]interface{}); ok {
				copy := map[string]interface{}{"type": "json_schema", "schema": schema["schema"]}
				return copy, true
			}
		}
		return format, true
	}
	if output, ok := request["output_config"].(map[string]interface{}); ok {
		if format, ok := output["format"].(map[string]interface{}); ok {
			return format, true
		}
	}
	if generation, ok := request["generationConfig"].(map[string]interface{}); ok {
		if schema, present := generation["responseJsonSchema"]; present {
			return map[string]interface{}{"type": "json_schema", "schema": schema}, true
		}
		if schema, present := generation["responseSchema"]; present {
			return map[string]interface{}{"type": "json_schema", "schema": schema}, true
		}
		if generation["responseMimeType"] == "application/json" {
			return map[string]interface{}{"type": "json_object"}, true
		}
	}
	return nil, false
}

func conversionMediaCount(request map[string]interface{}) int {
	total := 0
	var visit func(interface{})
	visit = func(value interface{}) {
		switch node := value.(type) {
		case map[string]interface{}:
			switch node["type"] {
			case "image", "image_url", "input_image", "document", "file", "input_file":
				total++
			}
			for key, child := range node {
				if key == "inlineData" || key == "fileData" {
					total++
					continue
				}
				visit(child)
			}
		case []interface{}:
			for _, child := range node {
				visit(child)
			}
		}
	}
	for _, key := range []string{"input", "messages", "contents", "system"} {
		visit(request[key])
	}
	return total
}
