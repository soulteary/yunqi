package converters

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestClaudeHostedSearchKeepsSourcesAndToolIdentity(t *testing.T) {
	raw := []byte(`{"content":[{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{"query":"example"}},{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://example.com/source","title":"Source title","encrypted_content":"DO_NOT_EXPOSE_OPAQUE"}]},{"type":"text","text":"Answer.","citations":[{"type":"web_search_result_location","url":"https://example.com/source","title":"Source title","cited_text":"Exact source quote"}]},{"type":"tool_use","id":"client_1","name":"lookup","input":{}}],"usage":{}}`)
	var input map[string]interface{}
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	converted, err := ClaudeResponseToResponses(input, "")
	if err != nil {
		t.Fatal(err)
	}
	calls, hosted := 0, 0
	for _, item := range converted.Output {
		if item.Type == "function_call" {
			calls++
			if item.CallID != "client_1" {
				t.Fatalf("hosted search became client tool: %#v", item)
			}
		}
		if item.Type == "web_search_call" {
			hosted++
			if item.ID != "srv_1" {
				t.Fatalf("hosted ID lost: %#v", item)
			}
		}
	}
	if calls != 1 || hosted != 1 {
		t.Fatalf("client=%d hosted=%d", calls, hosted)
	}
	output, _ := json.Marshal(converted)
	for _, value := range []string{"srv_1", "https://example.com/source", "Exact source quote", "hosted_search_sources_preserved_as_text"} {
		if !strings.Contains(string(output), value) {
			t.Fatalf("lost %s: %s", value, output)
		}
	}
	if strings.Contains(string(output), "DO_NOT_EXPOSE_OPAQUE") {
		t.Fatalf("opaque provider payload exposed: %s", output)
	}
}

func TestClaudeSearchCitationsStreamFallback(t *testing.T) {
	var state any
	var events []string
	for _, line := range []string{
		`data: {"type":"message_start","message":{"id":"r"}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srv_1","name":"web_search"}}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://example.com/source","title":"Title","encrypted_content":"DO_NOT_EXPOSE_OPAQUE"}]}}`,
		`data: {"type":"content_block_stop","index":1}`,
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"text","text":"Answer."}}`,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"citations_delta","citation":{"type":"search_result_location","source":"kb://article-1","title":"Internal source","cited_text":"Exact quoted text"}}}`,
		`data: {"type":"message_stop"}`,
	} {
		events = append(events, ConvertClaudeMessagesToResponses(context.Background(), "m", nil, nil, []byte(line), &state)...)
	}
	joined := strings.Join(events, "")
	for _, value := range []string{"srv_1", "https://example.com/source", "kb://article-1", "Exact quoted text", "hosted_search_sources_preserved_as_text"} {
		if !strings.Contains(joined, value) {
			t.Fatalf("lost %s: %s", value, joined)
		}
	}
	if strings.Contains(joined, "DO_NOT_EXPOSE_OPAQUE") || strings.Contains(joined, `"type":"function_call"`) {
		t.Fatalf("search semantics corrupted %s", joined)
	}
}
