package chat

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/types"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatClaudeSearchSourcesRemainReadable(t *testing.T) {
	var response map[string]interface{}
	if err := json.Unmarshal([]byte(`{"content":[{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://example.com","title":"Title"}]},{"type":"text","text":"Answer","citations":[{"url":"https://example.com","title":"Title","cited_text":"Quote"}]}]}`), &response); err != nil {
		t.Fatal(err)
	}
	converted := convertClaudeResponseToChat(response, "m")
	encoded, _ := json.Marshal(converted)
	for _, value := range []string{"srv_1", "https://example.com", "Quote"} {
		if !strings.Contains(string(encoded), value) {
			t.Fatalf("lost %s: %s", value, encoded)
		}
	}
	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	var usage *types.Usage
	done := false
	processClaudeChatStreamLine(c, w, "m", `data: {"type":"content_block_delta","delta":{"type":"citations_delta","citation":{"url":"https://example.com","title":"Title","cited_text":"Quote"}}}`, &usage, &done)
	if !strings.Contains(w.Body.String(), "https://example.com") || !strings.Contains(w.Body.String(), "Quote") {
		t.Fatalf("stream citation lost %s", w.Body.String())
	}
}
