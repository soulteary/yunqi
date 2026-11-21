package autopilot

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/redaction"
	"net/http"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gorilla/websocket"
	"github.com/soulteary/yunqi/internal/errutil"
	"github.com/soulteary/yunqi/internal/eventbus"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// ── Phase B.1：跨模块状态事件 API ──

// StateEventsResponse GET /api/health-center/state-events 返回结构。
type StateEventsResponse struct {
	Events []eventbus.Event `json:"events"`
	Total  int              `json:"total"`
}

// handleStateEvents GET /api/health-center/state-events
// 查询参数：
//   - limit=N         返回最近 N 条（默认 50，最大 200）
//   - subject=xxx     只返回指定 subject（channelUID / metricsKey / logicalChannelUid）
func handleStateEvents(mgr *Manager) fiber.Handler {
	return func(c fiber.Ctx) error {
		store := mgr.StateEventStore()
		if mgr == nil || store == nil {
			httpbridge.JSON(c, http.StatusOK, StateEventsResponse{Events: []eventbus.Event{}, Total: 0})
			return nil
		}
		limit := 50
		if limitStr := httpbridge.Query(c, "limit"); limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		if limit > 200 {
			limit = 200
		}
		var events []eventbus.Event
		if subject := httpbridge.Query(c, "subject"); subject != "" {
			ptrs := store.ListBySubject(subject, limit)
			events = make([]eventbus.Event, 0, len(ptrs))
			for _, p := range ptrs {
				events = append(events, *p)
			}
		} else {
			ptrs := store.ListRecent(limit)
			events = make([]eventbus.Event, 0, len(ptrs))
			for _, p := range ptrs {
				events = append(events, *p)
			}
		}
		httpbridge.JSON(c, http.StatusOK, StateEventsResponse{Events: events, Total: len(events)})
		return nil
	}
}

// stateEventsStreamUpgrader 与 changelogEventsUpgrader 共享同源策略。
var stateEventsStreamUpgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

const stateEventsStreamWriteTimeout = 15 * time.Second

// handleStateEventsStream GET /api/health-center/state-events/stream（WebSocket）
// 推送 eventbus.Event：circuit_breaker_state_changed / key_blacklisted / key_restored /
// key_model_disabled / key_model_restored 等。纯只读广播，不接收/处理客户端消息。
// 鉴权沿用 WebAuthMiddleware（已支持 Sec-WebSocket-Protocol 子协议回退）。
func handleStateEventsStream(mgr *Manager) fiber.Handler {
	return func(c fiber.Ctx) error {
		bus := mgr.EventBus()
		if mgr == nil || bus == nil {
			httpbridge.Status(c, http.StatusServiceUnavailable)
			return nil
		}

		// 回显子协议（同 changelog handler）
		var respHeader http.Header
		if proto := httpbridge.GetHeader(c, "Sec-WebSocket-Protocol"); proto != "" {
			respHeader = http.Header{"Sec-WebSocket-Protocol": []string{proto}}
		}

		conn, err := stateEventsStreamUpgrader.Upgrade(httpbridge.Writer(c), httpbridge.Request(c), respHeader)
		if err != nil {
			return nil
		}
		defer errutil.IgnoreDeferred(conn.Close)

		ch, unsubscribe := bus.Subscribe()
		defer unsubscribe()

		// 起 goroutine 消费/丢弃客户端消息，仅用于检测连接关闭
		closed := make(chan struct{})
		go func() {
			defer close(closed)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		for {
			select {
			case <-closed:
				return nil
			case ev, ok := <-ch:
				if !ok {
					return nil
				}
				_ = conn.SetWriteDeadline(time.Now().Add(stateEventsStreamWriteTimeout))
				payload, err := json.Marshal(ev)
				if err != nil {
					return nil
				}
				if err := conn.WriteMessage(websocket.TextMessage, redaction.JSON(payload)); err != nil {
					return nil
				}
			}
		}
	}
}
