package converters

import (
	"encoding/json"
	"fmt"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/tidwall/gjson"
	"strings"
)

// chatToolOutput separates media into a following user message because Chat's
// tool role accepts text, while keeping the original tool call/result pairing.
func chatToolOutput(output interface{}) (string, []interface{}) {
	parts, ok := output.([]interface{})
	if !ok {
		if text, ok := output.(string); ok {
			return text, nil
		}
		if output == nil {
			return "", nil
		}
		raw, _ := json.Marshal(output)
		return string(raw), nil
	}
	recognized := false
	var texts []string
	var media []interface{}
	for _, value := range parts {
		part, ok := value.(map[string]interface{})
		if !ok {
			raw, _ := json.Marshal(value)
			texts = append(texts, string(raw))
			continue
		}
		kind, _ := part["type"].(string)
		switch kind {
		case "text", "input_text", "output_text":
			recognized = true
			if text, ok := part["text"].(string); ok {
				texts = append(texts, text)
			}
		case "input_image", "image", "image_url":
			recognized = true
			raw, _ := json.Marshal(part)
			block := responsesImageContentToChatBlock(gjson.ParseBytes(raw))
			if block != "" {
				var decoded interface{}
				_ = json.Unmarshal([]byte(block), &decoded)
				media = append(media, decoded)
			}
		case "input_file", "file", "document":
			recognized = true
			file := map[string]interface{}{}
			if nested, ok := part["file"].(map[string]interface{}); ok {
				for key, value := range nested {
					file[key] = value
				}
			}
			for _, key := range []string{"file_data", "filename", "file_id"} {
				if value, present := part[key]; present {
					file[key] = value
				}
			}
			if source, ok := part["source"].(map[string]interface{}); ok && source["type"] == "base64" {
				mime, _ := source["media_type"].(string)
				data, _ := source["data"].(string)
				file["file_data"] = "data:" + mime + ";base64," + data
				if title, ok := part["title"].(string); ok {
					file["filename"] = title
				}
			}
			if url, ok := part["file_url"].(string); ok && url != "" {
				media = append(media, map[string]interface{}{"type": "text", "text": "Tool file URL: " + url})
				continue
			}
			media = append(media, map[string]interface{}{"type": "file", "file": file})
		default:
			raw, _ := json.Marshal(part)
			texts = append(texts, string(raw))
		}
	}
	if !recognized {
		raw, _ := json.Marshal(output)
		return string(raw), nil
	}
	return strings.Join(texts, "\n"), media
}

func expandChatToolOutputMedia(messages []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(messages))
	var pending []interface{}
	flush := func() {
		if len(pending) > 0 {
			out = append(out, map[string]interface{}{"role": "user", "content": pending})
			pending = nil
		}
	}
	for _, message := range messages {
		if message["role"] != "tool" {
			flush()
			out = append(out, message)
			continue
		}
		text, media := chatToolOutput(message["content"])
		message["content"] = text
		out = append(out, message)
		if len(media) > 0 {
			pending = append(pending, map[string]interface{}{"type": "text", "text": fmt.Sprintf("Media from tool result %v:", message["tool_call_id"])})
			pending = append(pending, media...)
		}
	}
	flush()
	return out
}

// ConvertToolOutputToClaude preserves structured media and rejects nonportable
// provider file IDs instead of silently turning binary data into tool text.
func ConvertToolOutputToClaude(output interface{}) (interface{}, error) {
	parts, ok := output.([]interface{})
	if !ok {
		return output, nil
	}
	converted := make([]interface{}, 0, len(parts))
	for _, value := range parts {
		part, ok := value.(map[string]interface{})
		if !ok {
			converted = append(converted, value)
			continue
		}
		kind, _ := part["type"].(string)
		switch kind {
		case "input_text", "output_text":
			copy := map[string]interface{}{"type": "text", "text": part["text"]}
			converted = append(converted, copy)
		case "input_image", "image_url":
			text, media := chatToolOutput([]interface{}{part})
			_ = text
			if len(media) != 1 {
				return nil, fmt.Errorf("tool image cannot be represented by Claude")
			}
			block := media[0].(map[string]interface{})
			image := block["image_url"].(map[string]interface{})
			url, _ := image["url"].(string)
			source := claudeMediaSource(url)
			converted = append(converted, map[string]interface{}{"type": "image", "source": source})
		case "input_file", "file":
			file := part
			if nested, ok := part["file"].(map[string]interface{}); ok {
				file = nested
			}
			data, _ := file["file_data"].(string)
			if data == "" {
				return nil, fmt.Errorf("tool file requires inline data for Claude conversion; provider file IDs are not portable")
			}
			source := claudeMediaSource(data)
			if !strings.HasPrefix(data, "data:") {
				source = map[string]interface{}{"type": "base64", "media_type": "application/pdf", "data": data}
			}
			converted = append(converted, map[string]interface{}{"type": "document", "source": source})
		default:
			converted = append(converted, part)
		}
	}
	return converted, nil
}

func claudeMediaSource(value string) map[string]interface{} {
	if header, data, ok := strings.Cut(value, ","); ok && strings.HasPrefix(header, "data:") && strings.HasSuffix(header, ";base64") {
		return map[string]interface{}{"type": "base64", "media_type": strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), "data": data}
	}
	return map[string]interface{}{"type": "url", "url": value}
}

func chatToolContentToResponses(content gjson.Result) string {
	out := make([]interface{}, 0)
	content.ForEach(func(_, block gjson.Result) bool {
		var part map[string]interface{}
		if json.Unmarshal([]byte(block.Raw), &part) != nil {
			return true
		}
		switch part["type"] {
		case "text":
			part["type"] = "input_text"
		case "image_url":
			part["type"] = "input_image"
			if image, ok := part["image_url"].(map[string]interface{}); ok {
				part["image_url"] = image["url"]
				if detail, present := image["detail"]; present {
					part["detail"] = detail
				}
			}
		case "file":
			part["type"] = "input_file"
			if file, ok := part["file"].(map[string]interface{}); ok {
				delete(part, "file")
				for key, value := range file {
					part[key] = value
				}
			}
		}
		out = append(out, part)
		return true
	})
	raw, _ := json.Marshal(out)
	return string(raw)
}

func geminiToolOutputMedia(output interface{}) (interface{}, []types.GeminiFunctionResponsePart) {
	text, media := chatToolOutput(output)
	if len(media) == 0 {
		return buildGeminiFunctionResponsePayload(output), nil
	}
	var parts []types.GeminiFunctionResponsePart
	refs := make([]interface{}, 0, len(media))
	for _, value := range media {
		block, _ := value.(map[string]interface{})
		url := ""
		switch block["type"] {
		case "image_url":
			image, _ := block["image_url"].(map[string]interface{})
			url, _ = image["url"].(string)
		case "file":
			file, _ := block["file"].(map[string]interface{})
			url, _ = file["file_data"].(string)
		}
		source := claudeMediaSource(url)
		if source["type"] != "base64" {
			continue
		}
		name := fmt.Sprintf("tool_media_%d", len(parts))
		mime, _ := source["media_type"].(string)
		data, _ := source["data"].(string)
		parts = append(parts, types.GeminiFunctionResponsePart{InlineData: &types.GeminiInlineData{MimeType: mime, Data: data, DisplayName: name}})
		refs = append(refs, map[string]interface{}{"$ref": name})
	}
	if len(parts) != len(media) {
		return buildGeminiFunctionResponsePayload(output), nil
	}
	return map[string]interface{}{"output": text, "media": refs}, parts
}

func geminiFunctionResponseOutput(response *types.GeminiFunctionResponse) interface{} {
	if len(response.Parts) == 0 {
		return response.Response
	}
	var parts []interface{}
	payload := response.Response
	if object, ok := payload.(map[string]interface{}); ok {
		if output, present := object["output"]; present {
			payload = output
		}
	}
	if text, ok := payload.(string); ok {
		if text != "" {
			parts = append(parts, map[string]interface{}{"type": "input_text", "text": text})
		}
	} else if payload != nil {
		raw, _ := json.Marshal(payload)
		parts = append(parts, map[string]interface{}{"type": "input_text", "text": string(raw)})
	}
	for _, part := range response.Parts {
		if data := part.InlineData; data != nil {
			url := "data:" + data.MimeType + ";base64," + data.Data
			if strings.HasPrefix(data.MimeType, "image/") {
				parts = append(parts, map[string]interface{}{"type": "input_image", "image_url": url})
			} else {
				parts = append(parts, map[string]interface{}{"type": "input_file", "file_data": url, "filename": data.DisplayName})
			}
		}
		if file := part.FileData; file != nil {
			parts = append(parts, map[string]interface{}{"type": "input_file", "file_url": file.FileURI, "mime_type": file.MimeType})
		}
	}
	return parts
}
