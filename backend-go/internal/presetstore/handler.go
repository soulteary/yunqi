package presetstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// Handler 返回 GET /api/presets 的 Fiber 处理器。
//
// 前端订阅表单从此端点派生来源类型/计费模式/来源等选项，替代前端硬编码副本。
// 响应带 ETag（基于 bundle JSON 内容哈希），前端携 If-None-Match 命中时返回 304，
// 避免预置未变时重复传输。store 为 nil 时回退进程默认。
func Handler(store *PresetStore) fiber.Handler {
	if store == nil {
		store = Default()
	}
	return func(c fiber.Ctx) error {
		bundle := store.Get()
		body, err := json.Marshal(bundle)
		if err != nil {
			httpbridge.JSON(c, http.StatusInternalServerError, fiber.Map{"error": "preset 序列化失败"})
			return nil
		}

		sum := sha256.Sum256(body)
		etag := `"` + hex.EncodeToString(sum[:]) + `"`
		if match := httpbridge.GetHeader(c, "If-None-Match"); match == etag {
			httpbridge.Status(c, http.StatusNotModified)
			return nil
		}
		httpbridge.Header(c, "ETag", etag)
		httpbridge.Header(c, "Cache-Control", "no-cache")
		// 允许缓存但每次校验 ETag
		httpbridge.Data(c, http.StatusOK, "application/json; charset=utf-8", body)
		return nil
	}
}
