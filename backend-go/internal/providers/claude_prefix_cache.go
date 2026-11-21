package providers

// applyClaudePrefixCache adds one stable multi-turn prefix breakpoint. Explicit
// client breakpoints always win; never infer an OpenAI retention policy or TTL.
func applyClaudePrefixCache(request map[string]interface{}) {
	if hasClaudeCacheControl(request) {
		return
	}
	messages, ok := request["messages"].([]interface{})
	if !ok || len(messages) < 3 {
		return
	}
	last, ok := messages[len(messages)-1].(map[string]interface{})
	if !ok || last["role"] != "user" {
		return
	}
	previous, ok := messages[len(messages)-2].(map[string]interface{})
	if !ok || previous["role"] != "assistant" {
		return
	}
	switch content := previous["content"].(type) {
	case string:
		if content == "" {
			return
		}
		previous["content"] = []interface{}{map[string]interface{}{"type": "text", "text": content, "cache_control": map[string]interface{}{"type": "ephemeral"}}}
	case []interface{}:
		if len(content) == 0 {
			return
		}
		block, ok := content[len(content)-1].(map[string]interface{})
		if !ok {
			return
		}
		switch block["type"] {
		case "text", "tool_use":
			block["cache_control"] = map[string]interface{}{"type": "ephemeral"}
		}
	}
}

func hasClaudeCacheControl(value interface{}) bool {
	switch node := value.(type) {
	case map[string]interface{}:
		if _, present := node["cache_control"]; present {
			return true
		}
		for _, child := range node {
			if hasClaudeCacheControl(child) {
				return true
			}
		}
	case []interface{}:
		for _, child := range node {
			if hasClaudeCacheControl(child) {
				return true
			}
		}
	}
	return false
}
