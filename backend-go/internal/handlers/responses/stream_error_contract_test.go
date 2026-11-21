package responses

import (
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/converters"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/types"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResponsesCommittedErrorShapesNeverBecomeSuccess(t *testing.T) {
	prefixes := map[string]string{
		"responses": "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n",
		"openai":    "data: {\"id\":\"r\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n",
		"claude":    "data: {\"type\":\"message_start\",\"message\":{\"id\":\"r\",\"model\":\"m\"}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n",
		"gemini":    "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\n",
	}
	errors := []string{
		`data: {"type":"error","code":"server_error","message":"failed"}`,
		`data: {"type":"response.error","response":{"error":{"code":"server_error","message":"failed"}}}`,
		`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"failed"}}}`,
		`data: {"type":"response.completed","response":{"status":"completed","error":{"code":"server_error","message":"failed"}}}`,
		`data: {"error":{"code":500,"message":"failed","status":"INTERNAL"}}`,
		"event: error\ndata: {\"code\":\"server_error\",\"message\":\"failed\"}",
	}
	for protocol, prefix := range prefixes {
		for _, failure := range errors {
			t.Run(protocol+failure, func(t *testing.T) {
				w := httptest.NewRecorder()
				c, _ := httpbridge.TestContext(w)
				httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
				raw := prefix + failure + "\n\ndata: [DONE]\n\n"
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(raw))}
				_, err := handleStreamSuccess(c, resp, protocol, &config.EnvConfig{}, nil, time.Now(), &types.ResponsesRequest{Model: "m"}, []byte(`{"model":"m","input":"hi"}`), common.StreamPreflightTimeouts{})
				if err == nil {
					t.Fatalf("error converted to success: %s", w.Body.String())
				}
				body := w.Body.String()
				if strings.Contains(body, "response.completed") || !strings.Contains(body, "response.failed") || !strings.Contains(body, `"source":"upstream"`) {
					t.Fatalf("wrong error terminal %s", body)
				}
				if strings.Count(body, "hello") != 1 {
					t.Fatalf("visible content replayed: %s", body)
				}
			})
		}
	}
}

func TestLocalIncompleteHasGatewaySource(t *testing.T) {
	event := converters.SynthesizeResponsesIncomplete(1, "r", "stream_stalled")
	if !strings.Contains(event, `"source":"gateway"`) {
		t.Fatalf("missing local provenance %s", event)
	}
}

func TestResponsesConvertedGeminiFailureCannotBeFollowedBySuccess(t *testing.T) {
	prefix := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\n"
	raw := prefix + "data: {\"candidates\":[{\"content\":{\"parts\":[]},\"finishReason\":\"SAFETY\"}]}\n\n"
	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}
	_, err := handleStreamSuccess(c, resp, "gemini", &config.EnvConfig{}, nil, time.Now(), &types.ResponsesRequest{Model: "m"}, []byte(`{"model":"m","input":"hi"}`), common.StreamPreflightTimeouts{})
	if err == nil || strings.Contains(w.Body.String(), `"type":"response.completed"`) {
		t.Fatalf("converted failure became success: %v %s", err, w.Body.String())
	}
}
