package providers

import (
	"context"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"testing"
)

func TestResponsesStructuredOutputWire(t *testing.T) {
	schema := `{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"integer"}},"additionalProperties":false}`
	raw := []byte(`{"model":"m","input":"hello","reasoning":{"effort":"high"},"text":{"format":{"type":"json_schema","name":"result","strict":true,"schema":` + schema + `}}}`)
	for service, path := range map[string]string{"claude": "output_config.format.schema", "openai": "response_format.json_schema.schema", "responses": "text.format.schema", "gemini": "generationConfig.responseJsonSchema"} {
		t.Run(service, func(t *testing.T) {
			c := newFiberContext(http.MethodPost, "/v1/responses", raw, context.Background())
			req, _, err := (&ResponsesProvider{}).ConvertToProviderRequest(c, &config.UpstreamConfig{BaseURL: "https://example.invalid", ServiceType: service, TextVerbosity: "low"}, "test")
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			if got := gjson.GetBytes(body, path).Raw; got != schema {
				t.Fatalf("wire schema %s; body %s", got, body)
			}
		})
	}
}
