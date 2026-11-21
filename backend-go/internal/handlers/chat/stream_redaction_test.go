package chat

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/redaction"
)

func TestChatPassthroughRedactsErrorCredentialsSplitAcrossNetworkChunks(t *testing.T) {
	const secret = "opaque!vendor-key=split+across/chunks"
	redaction.Configure([]string{secret})
	defer redaction.Configure(nil)
	for _, terminated := range []bool{false, true} {
		t.Run(map[bool]string{false: "EOF without newline", true: "terminated"}[terminated], func(t *testing.T) {
			input := "event: error\ndata: {\"message\":\"" + secret + "\"}"
			if terminated {
				input += "\n\n"
			}
			app := httpbridge.NewApp()
			app.Get("/stream", func(c fiber.Ctx) error {
				httpbridge.Header(c, "Content-Type", "text/event-stream")
				resp := &http.Response{Body: io.NopCloser(iotest.OneByteReader(strings.NewReader(input)))}
				progress := common.NewStreamProgressLogger("test", time.Now(), false)
				_, err := streamPassthrough(c, resp, httpbridge.Writer(c), nil, false, nil, common.StreamPreflightTimeouts{InactivityTimeoutMs: 1000}, progress)
				return err
			})
			response := httptest.NewRecorder()
			httpbridge.Handler(app).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/stream", nil))
			if strings.Contains(response.Body.String(), secret) || !strings.Contains(response.Body.String(), redaction.Mask) || !strings.HasSuffix(response.Body.String(), "\n\n") {
				t.Fatalf("unsafe SSE output: %s", response.Body.String())
			}
		})
	}
}
