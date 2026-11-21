package handlers

import (
	"context"
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"strconv"
	"time"
)

// GetRequestHistory is mounted behind the management authentication group.
// since/until are mandatory Unix seconds, keeping cursors stable between calls.
func GetRequestHistory(store *metrics.SQLiteStore) fiber.Handler {
	return func(c fiber.Ctx) error {
		reply := func(code int, v any) error { httpbridge.JSON(c, code, v); return nil }
		if store == nil {
			return reply(503, fiber.Map{"error": "历史持久化未启用"})
		}
		since, e1 := strconv.ParseInt(httpbridge.Query(c, "since", ""), 10, 64)
		until, e2 := strconv.ParseInt(httpbridge.Query(c, "until", ""), 10, 64)
		limit, e3 := strconv.Atoi(httpbridge.Query(c, "limit", "50"))
		if e1 != nil || e2 != nil || e3 != nil || since < 0 || until < since || limit < 1 || limit > 200 {
			return reply(400, fiber.Map{"error": "since/until 必须为 Unix 秒，limit 为 1–200"})
		}
		q := metrics.RequestHistoryQuery{APIType: httpbridge.Query(c, "type", "messages"), Model: httpbridge.Query(c, "model", ""), ChannelUID: httpbridge.Query(c, "channelUid", ""), CorrelationID: httpbridge.Query(c, "correlationId", ""), Since: time.Unix(since, 0), Until: time.Unix(until, 0), Limit: limit, Cursor: httpbridge.Query(c, "cursor", "")}
		// Validate input errors separately without exposing database diagnostics.
		switch q.APIType {
		case "messages", "responses", "chat", "gemini", "images", "vectors":
		default:
			return reply(400, fiber.Map{"error": "无效的 type"})
		}
		ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
		defer cancel()
		page, err := store.QueryRequestHistory(ctx, q)
		if err != nil {
			if errors.Is(err, metrics.ErrHistoryCursor) {
				return reply(400, fiber.Map{"error": "无效或与筛选条件不匹配的游标"})
			}
			return reply(500, fiber.Map{"error": "历史查询失败"})
		}
		return reply(200, page)
	}
}
