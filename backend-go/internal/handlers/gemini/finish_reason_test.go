package gemini

import (
	"errors"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGeminiOnlyThoughtAndMalformedToolAreNotDeliverable(t *testing.T) {
	for _, raw := range []string{
		`{"candidates":[{"content":{"parts":[{"text":"thinking","thought":true}]},"finishReason":"STOP"}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":null}]},"finishReason":"STOP"}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":""}}]},"finishReason":"STOP"}]}`,
	} {
		if hasGeminiSemanticContent(raw) {
			t.Fatalf("invalid deliverable: %s", raw)
		}
		resp := &http.Response{Body: io.NopCloser(strings.NewReader("data: " + raw + "\n\n"))}
		_, _, _, err := preflightGeminiStream(resp, "gemini", common.StreamPreflightTimeouts{})
		if !errors.Is(err, common.ErrEmptyStreamResponse) {
			t.Fatalf("error %v for %s", err, raw)
		}
	}
}

func TestGeminiAbnormalFinishReasonFailsPreflight(t *testing.T) {
	for _, reason := range []string{"MALFORMED_RESPONSE", "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL", "OTHER"} {
		raw := `data: {"candidates":[{"content":{"parts":[{"text":"partial"}]},"finishReason":"` + reason + `"}]}` + "\n\n"
		resp := &http.Response{Body: io.NopCloser(strings.NewReader(raw))}
		_, _, _, err := preflightGeminiStream(resp, "gemini", common.StreamPreflightTimeouts{})
		if !errors.Is(err, common.ErrEmptyStreamResponse) {
			t.Fatalf("accepted %s: %v", reason, err)
		}
	}
}

func TestGeminiNonStreamRejectsThoughtOnlyAndMalformedFinish(t *testing.T) {
	for _, raw := range []string{
		`{"candidates":[{"content":{"parts":[{"text":"thinking","thought":true}]},"finishReason":"STOP"}]}`,
		`{"candidates":[{"content":{"parts":[{"text":"partial"}]},"finishReason":"MALFORMED_RESPONSE"}]}`,
	} {
		w := httptest.NewRecorder()
		c, _ := httpbridge.TestContext(w)
		httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1beta/models/m:generateContent", nil))
		resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}
		_, err := handleSuccess(c, resp, "gemini", &config.EnvConfig{}, time.Now(), nil, "m", false, common.StreamPreflightTimeouts{})
		if !errors.Is(err, common.ErrEmptyNonStreamResponse) {
			t.Fatalf("error %v; body %s", err, w.Body.String())
		}
		if w.Body.Len() != 0 {
			t.Fatalf("invalid response committed: %s", w.Body.String())
		}
	}
}
