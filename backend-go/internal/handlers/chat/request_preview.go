package chat

import (
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"net/http"
)

// BuildRequestPreview uses the live request builder without executing a request.
func BuildRequestPreview(c fiber.Ctx, upstream *config.UpstreamConfig, key string, body []byte, model string, stream bool) (*http.Request, error) {
	return buildProviderRequest(c, upstream, upstream.BaseURL, key, body, model, stream)
}
