package providers

import (
	"context"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"testing"
)

func TestClaudeToolMediaActualResponsesRequest(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":[{"type":"text","text":"ready"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aW1hZ2U="}}]},{"type":"tool_result","tool_use_id":"b","content":[{"type":"document","title":"r.pdf","source":{"type":"base64","media_type":"application/pdf","data":"cGRm"}}]}]}]}`)
	c := newFiberContext(http.MethodPost, "/v1/messages", raw, context.Background())
	req, _, err := (&ResponsesProvider{}).ConvertToProviderRequest(c, &config.UpstreamConfig{BaseURL: "https://example.invalid", ServiceType: "responses"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	r := gjson.ParseBytes(body)
	if r.Get("input.0.call_id").String() != "a" || r.Get("input.1.call_id").String() != "b" || r.Get("input.0.output.1.image_url").String() != "data:image/png;base64,aW1hZ2U=" || r.Get("input.1.output.0.file_data").String() != "data:application/pdf;base64,cGRm" {
		t.Fatalf("media or pair lost %s", body)
	}
}
