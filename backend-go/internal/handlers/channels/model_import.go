package channels

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
)

func (h *Handler) importChannel(c fiber.Ctx, write bool) (string, bool) {
	uid := httpbridge.Param(c, "uid")
	action := "channels.read"
	if write {
		action = "channels.model-import"
	}
	if !middleware.AuthorizeAdminAction(c, action, uid) {
		return "", false
	}
	protocol := httpbridge.Query(c, "protocol", "")
	cfg := h.cm.GetConfig()
	matches := make([]string, 0, 1)
	for _, kind := range []config.ChannelKind{config.ChannelKindMessages, config.ChannelKindChat, config.ChannelKindResponses, config.ChannelKindGemini, config.ChannelKindImages, config.ChannelKindVectors} {
		if protocol != "" && protocol != string(kind) {
			continue
		}
		for _, upstream := range config.ChannelKindRegistry[kind].Slice(cfg) {
			if upstream.ChannelUID == uid || upstream.LogicalChannelUID == uid {
				matches = append(matches, upstream.ChannelUID)
			}
		}
	}
	if len(matches) == 0 {
		httpbridge.JSON(c, 404, fiber.Map{"error": "model import channel not found"})
		return "", false
	}
	if len(matches) != 1 {
		httpbridge.JSON(c, 400, fiber.Map{"error": "select a single channel protocol for model import"})
		return "", false
	}
	return matches[0], true
}
func decodeModelImport(c fiber.Ctx, target interface{}, limit int64) bool {
	raw, err := io.ReadAll(io.LimitReader(httpbridge.Request(c).Body, limit+1))
	if err != nil || int64(len(raw)) > limit {
		httpbridge.JSON(c, 400, fiber.Map{"error": "model import input exceeds size limit"})
		return false
	}
	if _, policy := target.(*config.ModelImportPolicy); policy {
		var fields map[string]json.RawMessage
		var enabled bool
		if json.Unmarshal(raw, &fields) != nil || len(fields["enabled"]) == 0 || bytes.Equal(bytes.TrimSpace(fields["enabled"]), []byte("null")) || json.Unmarshal(fields["enabled"], &enabled) != nil {
			httpbridge.JSON(c, 400, fiber.Map{"error": "model import policy requires an explicit enabled boolean"})
			return false
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		httpbridge.JSON(c, 400, fiber.Map{"error": "invalid model import input"})
		return false
	}
	var extra interface{}
	if decoder.Decode(&extra) != io.EOF {
		httpbridge.JSON(c, 400, fiber.Map{"error": "invalid model import input"})
		return false
	}
	return true
}
func modelImportError(c fiber.Ctx, err error) error {
	status := 500
	message := "model import persistence failed"
	switch {
	case errors.Is(err, config.ErrModelImportNotFound):
		status = 404
		message = "model import channel not found"
	case errors.Is(err, config.ErrModelImportConflict):
		status = 409
		message = "model import review is stale; reload and preview again"
	case errors.Is(err, config.ErrInvalidModelImport):
		status = 400
		message = err.Error()
	}
	httpbridge.JSON(c, status, fiber.Map{"error": message})
	return nil
}
func (h *Handler) GetModelImport(c fiber.Ctx) error {
	uid, ok := h.importChannel(c, false)
	if !ok {
		return nil
	}
	view, err := h.cm.GetModelImport(uid)
	if err != nil {
		return modelImportError(c, err)
	}
	httpbridge.Header(c, "ETag", `"`+view.Revision+`"`)
	httpbridge.Header(c, "Cache-Control", "no-store")
	httpbridge.JSON(c, 200, view)
	return nil
}
func (h *Handler) SetModelImportPolicy(c fiber.Ctx) error {
	uid, ok := h.importChannel(c, true)
	if !ok {
		return nil
	}
	var policy config.ModelImportPolicy
	if !decodeModelImport(c, &policy, 65536) {
		return nil
	}
	view, err := h.cm.SetModelImportPolicy(uid, httpbridge.GetHeader(c, "If-Match"), policy, middleware.GetAdminActorID(c))
	if err != nil {
		return modelImportError(c, err)
	}
	httpbridge.Header(c, "ETag", `"`+view.Revision+`"`)
	httpbridge.JSON(c, 200, view)
	return nil
}
func (h *Handler) PreviewModelImport(c fiber.Ctx) error {
	uid, ok := h.importChannel(c, true)
	if !ok {
		return nil
	}
	var input struct {
		Models []config.ModelImportCandidate `json:"models"`
	}
	if !decodeModelImport(c, &input, 2<<20) {
		return nil
	}
	review, err := h.cm.StageModelImport(uid, input.Models, "operator", httpbridge.GetHeader(c, "If-Match"))
	if err != nil {
		return modelImportError(c, err)
	}
	httpbridge.Header(c, "Cache-Control", "no-store")
	httpbridge.JSON(c, 200, review)
	return nil
}
func (h *Handler) ApproveModelImport(c fiber.Ctx) error {
	uid, ok := h.importChannel(c, true)
	if !ok {
		return nil
	}
	var input struct {
		ReviewID      string `json:"reviewId"`
		AllowEmpty    bool   `json:"allowEmpty"`
		AllowRemovals bool   `json:"allowRemovals"`
	}
	if !decodeModelImport(c, &input, 4096) {
		return nil
	}
	view, err := h.cm.ApproveModelImport(uid, input.ReviewID, middleware.GetAdminActorID(c), input.AllowEmpty, input.AllowRemovals)
	if err != nil {
		return modelImportError(c, err)
	}
	httpbridge.Header(c, "ETag", `"`+view.Revision+`"`)
	httpbridge.JSON(c, 200, view)
	return nil
}
