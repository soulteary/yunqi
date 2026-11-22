package channels

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/redaction"
	"github.com/soulteary/yunqi/internal/requestpreview"
)

func (h *Handler) RequestPreview(c fiber.Ctx) error {
	uid := httpbridge.Param(c, "uid")
	if !middleware.AuthorizeAdminAction(c, "channels.read", uid) {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(httpbridge.Request(c).Body, 65537))
	if err != nil || len(data) > 65536 {
		httpbridge.JSON(c, 400, fiber.Map{"error": "preview input exceeds limit"})
		return nil
	}
	var input requestpreview.Input
	if err := json.Unmarshal(data, &input); err != nil {
		httpbridge.JSON(c, 400, fiber.Map{"error": "invalid preview input"})
		return nil
	}
	// Work from one deep immutable snapshot; never retain a pointer returned from
	// a separately unlocked channel lookup while another administrator saves.
	cfg := h.cm.GetConfig()
	var selected *config.UpstreamConfig
	for _, kind := range []config.ChannelKind{config.ChannelKindMessages, config.ChannelKindChat, config.ChannelKindResponses, config.ChannelKindGemini} {
		if string(kind) != input.Protocol {
			continue
		}
		for _, up := range config.ChannelKindRegistry[kind].Slice(cfg) {
			if up.ChannelUID == uid || up.LogicalChannelUID == uid {
				copy := up
				selected = &copy
				break
			}
		}
		if selected != nil {
			break
		}
	}
	if selected == nil {
		httpbridge.JSON(c, http.StatusNotFound, fiber.Map{"error": "preview requires an existing messages/chat/responses/gemini channel"})
		return nil
	}
	result, err := requestpreview.Build(*selected, input)
	httpbridge.Header(c, "Cache-Control", "no-store")
	if err != nil {
		httpbridge.JSON(c, 400, fiber.Map{"error": redaction.String(err.Error())})
		return nil
	}
	httpbridge.JSON(c, 200, result)
	return nil
}
