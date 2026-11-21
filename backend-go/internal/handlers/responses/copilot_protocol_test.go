package responses

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/providers"
	"github.com/soulteary/yunqi/internal/types"
)

// Copilot is an authentication/service identity. Its /responses endpoint emits
// the same wire protocol as a native Responses upstream, including cached usage.
func TestNativeResponsesProtocolAcrossServiceIdentities(t *testing.T) {
	const responseJSON = `{"id":"resp_native","model":"native-model","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13,"input_tokens_details":{"cached_tokens":4}}}`
	for _, serviceType := range []string{"responses", "copilot"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", serviceType, stream), func(t *testing.T) {
				store := false
				request := &types.ResponsesRequest{Model: "native-model", Input: "hello", Stream: stream, Store: &store}
				raw, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				writer := httptest.NewRecorder()
				ctx, _ := httpbridge.TestContext(writer)
				httpbridge.SetRequest(ctx, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(raw))))
				body := responseJSON
				contentType := "application/json"
				if stream {
					contentType = "text/event-stream"
					body = "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_native\",\"status\":\"in_progress\"}}\n\n" +
						"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n" +
						"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + responseJSON + "}\n\n"
				}
				upstreamResponse := &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}},
					Body: io.NopCloser(strings.NewReader(body)),
				}
				usage, err := handleSuccess(ctx, upstreamResponse, &providers.ResponsesProvider{},
					&config.UpstreamConfig{ServiceType: serviceType}, "test-key", serviceType,
					&config.EnvConfig{}, nil, time.Now(), request, raw, common.StreamPreflightTimeouts{})
				if err != nil {
					t.Fatalf("native response was rejected or converted as Chat: %v", err)
				}
				if usage == nil || usage.InputTokens != 10 || usage.OutputTokens != 3 || usage.CacheReadInputTokens != 4 || usage.PromptTokensTotal != 10 {
					t.Fatalf("native usage was reinterpreted: %+v", usage)
				}
				if stream {
					if !strings.Contains(writer.Body.String(), `"delta":"hello"`) || strings.Count(writer.Body.String(), "event: response.completed\n") != 1 {
						t.Fatalf("native SSE text/terminal events were lost or duplicated: %s", writer.Body.String())
					}
					return
				}
				var response types.ResponsesResponse
				if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.ID != "resp_native" || response.Usage.InputTokens != 10 || response.Usage.TotalTokens != 13 || response.Usage.InputTokensDetails == nil || response.Usage.InputTokensDetails.CachedTokens != 4 {
					t.Fatalf("native wire usage must remain inclusive of cached tokens: %+v", response)
				}
			})
		}
	}
}
