package responses

import (
	"errors"
	"github.com/soulteary/yunqi/internal/config"
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

type failingEOFReader struct{}

func (failingEOFReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestResponsesEOFRequiresTerminal(t *testing.T) {
	for _, tc := range []struct {
		name, tail     string
		reset, success bool
	}{
		{name: "partial EOF"},
		{name: "done without terminal", tail: "data: [DONE]\n\n"},
		{name: "connection reset", reset: true},
		{name: "explicit completed", tail: "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n", success: true},
		{name: "explicit incomplete", tail: "data: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"r\",\"status\":\"incomplete\"}}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefix := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\"}}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\" world\"}\n\n"
			var reader io.Reader = strings.NewReader(prefix + tc.tail)
			if tc.reset {
				reader = io.MultiReader(reader, failingEOFReader{})
			}
			w := httptest.NewRecorder()
			c, _ := httpbridge.TestContext(w)
			httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(reader)}
			_, err := handleStreamSuccess(c, resp, "responses", &config.EnvConfig{}, nil, time.Now(), &types.ResponsesRequest{Model: "m"}, []byte(`{"model":"m","input":"hi"}`), common.StreamPreflightTimeouts{})
			if tc.success {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, common.ErrStreamUnexpectedEOF) {
				t.Fatalf("error=%v", err)
			}
			body := w.Body.String()
			if strings.Contains(body, "response.completed") {
				t.Fatalf("fabricated success: %s", body)
			}
			if !strings.Contains(body, "response.incomplete") {
				t.Fatalf("missing incomplete: %s", body)
			}
			if strings.Count(body, `"delta":"hello"`) != 1 {
				t.Fatalf("visible replay: %s", body)
			}
		})
	}
}

func TestUpstreamTerminalContract(t *testing.T) {
	for _, tc := range []struct {
		protocol, line string
		want           bool
	}{
		{"responses", `data: [DONE]`, false},
		{"responses", `data: {"type":"response.in_progress"}`, false},
		{"responses", `data: {"type":"response.completed"}`, true},
		{"claude", `data: {"type":"message_stop"}`, true},
		{"claude", `data: {"type":"content_block_stop"}`, false},
		{"gemini", `data: {"candidates":[{"finishReason":"STOP"}]}`, true},
		{"gemini", `data: {"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`, false},
		{"openai", `data: {"choices":[{"finish_reason":"stop"}]}`, true},
		{"openai", `data: {"choices":[{"delta":{"content":"hi"},"finish_reason":null}]}`, false},
		{"openai", `data: [DONE]`, false},
	} {
		if got := hasUpstreamTerminalEvent(tc.line, tc.protocol); got != tc.want {
			t.Fatalf("%s %s: %v", tc.protocol, tc.line, got)
		}
	}
}

func TestConvertedStreamEOFDoesNotComplete(t *testing.T) {
	for protocol, raw := range map[string]string{
		"openai": "data: {\"id\":\"r\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"id\":\"r\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"}}]}\n\ndata: [DONE]\n\n",
		"claude": "data: {\"type\":\"message_start\",\"message\":{\"id\":\"r\",\"model\":\"m\"}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n",
		"gemini": "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\ndata: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\" world\"}]}}]}\n\n",
	} {
		t.Run(protocol, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := httpbridge.TestContext(w)
			httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(raw))}
			_, err := handleStreamSuccess(c, resp, protocol, &config.EnvConfig{}, nil, time.Now(), &types.ResponsesRequest{Model: "m"}, []byte(`{"model":"m","input":"hi"}`), common.StreamPreflightTimeouts{})
			if !errors.Is(err, common.ErrStreamUnexpectedEOF) {
				t.Fatalf("error %v; body %s", err, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "response.completed") || !strings.Contains(w.Body.String(), "response.incomplete") {
				t.Fatalf("wrong terminal %s", w.Body.String())
			}
		})
	}
}
