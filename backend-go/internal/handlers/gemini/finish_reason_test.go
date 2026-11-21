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

func TestGeminiNativeRefusalsAndMediaAreDeliverable(t *testing.T) {
	for _, raw := range []string{
		`{"candidates":[{"finishReason":"SAFETY"}]}`,
		`{"candidates":[{"finishReason":"IMAGE_SAFETY"}]}`,
		`{"promptFeedback":{"blockReason":"SAFETY"}}`,
		`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]},"finishReason":"STOP"}]}`,
		`{"candidates":[{"content":{"parts":[{"fileData":{"mimeType":"image/png","fileUri":"gs://bucket/image.png"}}]},"finishReason":"STOP"}]}`,
	} {
		if !hasGeminiSemanticContent(raw) {
			t.Fatalf("native outcome rejected %s", raw)
		}
		resp := &http.Response{Body: io.NopCloser(strings.NewReader("data: " + raw + "\n\n"))}
		_, _, _, err := preflightGeminiStream(resp, "gemini", common.StreamPreflightTimeouts{})
		if err != nil {
			t.Fatalf("native outcome retried: %v: %s", err, raw)
		}
		w := httptest.NewRecorder()
		c, _ := httpbridge.TestContext(w)
		httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1beta/models/m:streamGenerateContent", nil))
		response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + raw + "\n\n"))}
		if _, err := handleSuccess(c, response, "gemini", &config.EnvConfig{}, time.Now(), nil, "m", true, common.StreamPreflightTimeouts{FirstContentTimeoutMs: 100, InactivityTimeoutMs: 100}); err != nil {
			t.Fatalf("native outcome failed after preflight: %v", err)
		}
		if !strings.Contains(w.Body.String(), raw) {
			t.Fatalf("native outcome not forwarded: %s", w.Body.String())
		}

	}
	for _, raw := range []string{`{"candidates":[{"content":{"parts":[{"text":"  \n\t"}]},"finishReason":"STOP"}]}`, `{"candidates":[{"content":{"parts":[{"inlineData":{"data":""}}]},"finishReason":"STOP"}]}`} {
		if hasGeminiSemanticContent(raw) {
			t.Fatalf("empty data accepted %s", raw)
		}
	}
}

func TestGeminiInvalidFinishReasonTypesFailAtEveryStage(t *testing.T) {
	for _, value := range []string{`42`, `false`, `{}`} {
		t.Run(value, func(t *testing.T) {
			raw := `{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":` + value + `}]}`
			resp := &http.Response{Body: io.NopCloser(strings.NewReader("data: " + raw + "\n\n"))}
			if _, _, _, err := preflightGeminiStream(resp, "gemini", common.StreamPreflightTimeouts{}); !errors.Is(err, common.ErrEmptyStreamResponse) {
				t.Fatalf("preflight accepted %s: %v", value, err)
			}
			w := httptest.NewRecorder()
			c, _ := httpbridge.TestContext(w)
			httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1beta/models/m:generateContent", nil))
			response := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}
			if _, err := handleSuccess(c, response, "gemini", &config.EnvConfig{}, time.Now(), nil, "m", false, common.StreamPreflightTimeouts{}); !errors.Is(err, common.ErrInvalidResponseBody) {
				t.Fatalf("nonstream accepted %s: %v", value, err)
			}
			if w.Body.Len() != 0 {
				t.Fatalf("nonstream committed invalid response %s", w.Body.String())
			}
			prefix := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"first\"}]}}]}\n\n"
			reader := &streamLineReader{remainder: prefix + "data: " + raw + "\n\n", eof: true}
			progress := common.NewStreamProgressLogger("Gemini", time.Now(), false)
			if _, err := streamGeminiToGemini(c, reader, w, nil, false, common.StreamPreflightTimeouts{InactivityTimeoutMs: 100}, progress); err == nil {
				t.Fatalf("postcommit accepted %s", value)
			}
			if !strings.Contains(w.Body.String(), "finishReason must be a string") || strings.Contains(w.Body.String(), `"finishReason":`+value) {
				t.Fatalf("postcommit error not normalized %s", w.Body.String())
			}
		})
	}
}
