package gemini

import (
	"encoding/json"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/types"
	"net/http"
)

// BuildRequestPreview uses the live request builder without executing a request.
func BuildRequestPreview(c fiber.Ctx, upstream *config.UpstreamConfig, key string, body []byte, model string, stream bool) (*http.Request, error) {
	var parsed types.GeminiRequest
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	return buildProviderRequest(c, upstream, upstream.BaseURL, key, body, &parsed, model, stream)
}
