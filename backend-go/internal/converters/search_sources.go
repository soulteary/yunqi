package converters

import (
	"fmt"
	"strings"
)

// ClaudeSearchSourceText preserves public attribution when the destination has
// no equivalent hosted-search or source-block representation. Opaque provider
// encrypted payloads are deliberately not treated as user-facing citations.
func ClaudeSearchSourceText(block map[string]interface{}) string {
	kind, _ := block["type"].(string)
	switch kind {
	case "text":
		var out strings.Builder
		if citations, ok := block["citations"].([]interface{}); ok {
			for _, value := range citations {
				if citation, ok := value.(map[string]interface{}); ok {
					out.WriteString(ClaudeCitationSourceText(citation))
				}
			}
		}
		return out.String()
	case "search_result", "web_search_result":
		return renderSearchSource(block)
	case "web_search_tool_result":
		id, _ := block["tool_use_id"].(string)
		if code, failed := ClaudeHostedSearchError(block); failed {
			return "\n[Hosted web search result " + id + " failed: " + code + "]\n"
		}
		var out strings.Builder
		out.WriteString("\n[Hosted web search result " + id + "]\n")
		if results, ok := block["content"].([]interface{}); ok {
			for _, value := range results {
				if result, ok := value.(map[string]interface{}); ok {
					out.WriteString(renderSearchSource(result))
				}
			}
		}
		return out.String()
	}
	return ""
}

func ClaudeCitationSourceText(citation map[string]interface{}) string {
	return renderSearchSource(citation)
}

func renderSearchSource(source map[string]interface{}) string {
	url, _ := source["url"].(string)
	if url == "" {
		url, _ = source["source"].(string)
	}
	if strings.TrimSpace(url) == "" {
		return ""
	}
	title, _ := source["title"].(string)
	quote, _ := source["cited_text"].(string)
	if quote == "" {
		if content, ok := source["content"].([]interface{}); ok {
			var parts []string
			for _, value := range content {
				if block, ok := value.(map[string]interface{}); ok {
					if text, ok := block["text"].(string); ok && text != "" {
						parts = append(parts, text)
					}
				}
			}
			quote = strings.Join(parts, "\n")
		}
	}
	out := fmt.Sprintf("\n[Search source: %s]\nSource: %s\n", title, url)
	if quote != "" {
		out += "Cited text: " + quote + "\n"
	}
	return out
}

func HasClaudeSearchAttribution(value interface{}) bool {
	switch node := value.(type) {
	case map[string]interface{}:
		switch node["type"] {
		case "search_result", "web_search_tool_result":
			return true
		}
		if citations, ok := node["citations"].([]interface{}); ok && len(citations) > 0 {
			return true
		}
		for _, child := range node {
			if HasClaudeSearchAttribution(child) {
				return true
			}
		}
	case []interface{}:
		for _, child := range node {
			if HasClaudeSearchAttribution(child) {
				return true
			}
		}
	}
	return false
}

// ClaudeHostedSearchError exposes only allowlisted public error codes, never
// arbitrary provider error messages or opaque payloads.
func ClaudeHostedSearchError(block map[string]interface{}) (string, bool) {
	if block["type"] != "web_search_tool_result" {
		return "", false
	}
	content, ok := block["content"].(map[string]interface{})
	if !ok {
		return "", false
	}
	code, _ := content["error_code"].(string)
	if content["type"] != "web_search_tool_result_error" {
		return "invalid_hosted_search_result", true
	}
	switch code {
	case "max_uses_exceeded", "too_many_requests", "invalid_input", "query_too_long", "unavailable":
		return code, true
	}
	return "hosted_search_error", true
}
