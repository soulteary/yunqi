package providers

import (
	"context"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestClaudeSearchResultRequestKeepsSourceAndToolPair(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"client_1","name":"retrieve","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"client_1","content":[{"type":"search_result","source":"https://example.com","title":"Title","content":[{"type":"text","text":"Quoted source"}],"citations":{"enabled":true}}]}]}]}`)
	c := newFiberContext(http.MethodPost, "/v1/messages", raw, context.Background())
	req, _, err := (&ResponsesProvider{}).ConvertToProviderRequest(c, &config.UpstreamConfig{BaseURL: "https://example.invalid", ServiceType: "responses"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"client_1", "https://example.com", "Quoted source"} {
		if !strings.Contains(string(body), value) {
			t.Fatalf("lost %s in %s", value, body)
		}
	}
	if httpbridge.Writer(c).Header().Get("X-Yunqi-Search-Conversion") != "source_blocks_preserved_as_text" {
		t.Fatal("missing conversion diagnostic")
	}
}

func TestClaudeHostedSearchNeverBecomesClientFunctionTool(t *testing.T) {
	raw := []byte(`{"model":"m","tools":[{"type":"web_search_20250305","name":"web_search"}],"messages":[{"role":"user","content":"search"}]}`)
	c := newFiberContext(http.MethodPost, "/v1/messages", raw, context.Background())
	if _, _, err := (&ResponsesProvider{}).ConvertToProviderRequest(c, &config.UpstreamConfig{BaseURL: "https://example.invalid", ServiceType: "responses"}, "test"); err == nil {
		t.Fatal("hosted tool silently converted to client function")
	}
}

func TestClaudeCitationsOnlyTextStillKeepsSourceInActualRequest(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[{"role":"assistant","content":[{"type":"text","text":"","citations":[{"url":"https://example.com/citation","title":"Citation","cited_text":"Quoted source"}]}]}]}`)
	c := newFiberContext(http.MethodPost, "/v1/messages", raw, context.Background())
	req, _, err := (&ResponsesProvider{}).ConvertToProviderRequest(c, &config.UpstreamConfig{BaseURL: "https://example.invalid", ServiceType: "responses"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "https://example.com/citation") || !strings.Contains(string(body), "Quoted source") {
		t.Fatalf("citation-only source lost %s", body)
	}
}
