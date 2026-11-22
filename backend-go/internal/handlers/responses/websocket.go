package responses

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/soulteary/yunqi/internal/redaction"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/gorilla/websocket"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/errutil"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/telemetry"
	"github.com/soulteary/yunqi/internal/utils"
)

const (
	responsesWebSocketWriteTimeout      = 15 * time.Second
	responsesWebSocketReadLimit         = 32 << 20
	responsesWebSocketV2BetaHeader      = "OpenAI-Beta"
	responsesWebSocketV2BetaHeaderValue = "responses_websockets=2026-02-06"
)

var responsesWebSocketUpgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

// WebSocketHandler 支持 Codex 原生 Responses WebSocket 协议。
// 上游使用 HTTP/SSE bridge，并在同一连接中完整重放已知的续接历史。
func WebSocketHandler(
	envCfg *config.EnvConfig,
	cfgManager *config.ConfigManager,
	sessionManager *session.SessionManager,
	channelScheduler *scheduler.ChannelScheduler,
) fiber.Handler {
	httpHandler := Handler(envCfg, cfgManager, sessionManager, channelScheduler)
	bridgeRouter := httpbridge.NewApp()
	bridgeRouter.Use(telemetry.BridgeMiddleware())
	bridgeRouter.Use(middleware.ProxyAdmissionMiddleware(envCfg))
	bridgeRouter.Post("/v1/responses", httpHandler)
	bridgeRouter.Post("/:routePrefix/v1/responses", httpHandler)

	return fiber.Handler(func(c fiber.Ctx) error {
		if !isWebSocketUpgrade(httpbridge.Request(c)) {
			httpbridge.Status(c, http.StatusMethodNotAllowed)
			return nil
		}
		middleware.AuthenticateProxy(c, envCfg)
		if httpbridge.IsAborted(c) {
			return nil
		}

		conn, err := responsesWebSocketUpgrader.Upgrade(httpbridge.Writer(c), httpbridge.Request(c), nil)
		if err != nil {
			return nil
		}
		defer errutil.IgnoreDeferred(conn.Close)
		conn.SetReadLimit(responsesWebSocketReadLimit)

		for {
			messageType, payload, err := conn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					closeWithWebSocketError(conn, http.StatusBadRequest, "websocket_read_error", err.Error())
				}
				return nil
			}
			if messageType != websocket.TextMessage {
				closeWithWebSocketError(conn, http.StatusBadRequest, "invalid_message_type", "only text messages are supported")
				return nil
			}
			if _, err := parseWebSocketResponseCreatePayload(payload); err != nil {
				_ = writeWebSocketError(conn, http.StatusBadRequest, "invalid_request", err.Error())
				continue
			}

			// 下游保持 Codex 原生 responses_websockets 协议；上游统一走 HTTP/SSE bridge，
			// 这样原生 Responses 渠道也能复用 518n-2 continuation folding。
			serveResponsesWebSocketBridgeLoop(c, conn, httpbridge.Handler(bridgeRouter), payload)
			return nil
		}
	})
}

func isWebSocketUpgrade(r *http.Request) bool {
	return headerContainsToken(r.Header, "Connection", "upgrade") &&
		headerContainsToken(r.Header, "Upgrade", "websocket")
}

func headerContainsToken(header http.Header, key, token string) bool {
	for _, value := range header.Values(key) {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

func parseWebSocketResponseCreatePayload(payload []byte) (map[string]interface{}, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("解析 response.create 失败: %w", err)
	}

	msgType, _ := req["type"].(string)
	if msgType != "response.create" {
		return nil, fmt.Errorf("不支持的 WebSocket 消息类型: %s", msgType)
	}
	return req, nil
}

func normalizeWebSocketResponseCreatePayload(payload []byte) ([]byte, bool, error) {
	req, err := parseWebSocketResponseCreatePayload(payload)
	if err != nil {
		return nil, false, err
	}
	if model, present := req["model"]; present {
		if _, ok := model.(string); !ok {
			return nil, false, fmt.Errorf("model must be a string")
		}
	}
	warmup := req["generate"] == false

	delete(req, "type")
	delete(req, "client_metadata")
	delete(req, "generate")
	req["stream"] = true

	body, err := json.Marshal(req)
	if err != nil {
		return nil, false, fmt.Errorf("序列化 response.create 失败: %w", err)
	}
	return body, warmup, nil
}

func normalizeNativeWebSocketResponseCreatePayload(payload []byte) ([]byte, error) {
	req, err := parseWebSocketResponseCreatePayload(payload)
	if err != nil {
		return nil, err
	}
	req["stream"] = true

	body, err := utils.MarshalJSONNoEscape(req)
	if err != nil {
		return nil, fmt.Errorf("序列化 response.create 失败: %w", err)
	}
	return body, nil
}

func serveResponsesWebSocketBridgeLoop(
	c fiber.Ctx,
	conn *websocket.Conn,
	bridgeRouter http.Handler,
	firstPayload []byte,
) {
	// Read independently of HTTP/SSE processing so a disconnected client cancels
	// even an upstream that has not sent headers or content yet.
	ctx, cancel := context.WithCancel(httpbridge.Request(c).Context())
	var active webSocketActiveTurn
	active.reserve() // first response.create was accepted by the handshake reader
	messages := make(chan webSocketClientMessage, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer cancel()
		for {
			kind, body, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.TextMessage {
				var control struct {
					Type       string `json:"type"`
					ResponseID string `json:"response_id"`
				}
				if json.Unmarshal(body, &control) == nil && (control.Type == "response.cancel" || control.Type == "response.interrupt") {
					if active.interrupt(control.ResponseID) {
						continue
					}
				}
			}
			reserved := false
			if kind == websocket.TextMessage {
				var event struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(body, &event) == nil && event.Type == "response.create" {
					active.reserve()
					reserved = true
				}
			}
			select {
			case messages <- webSocketClientMessage{kind, body, reserved}:
			case <-ctx.Done():
				return
			default:
				// Bound queued input to one pending turn rather than retaining arbitrary
				// 32 MiB frames while inference is running. Control writes are concurrency-safe.
				_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "too many queued requests"), time.Now().Add(time.Second))
				return
			}
		}
	}()
	defer func() { cancel(); _ = conn.Close(); <-readerDone }()
	payload := firstPayload
	payloadReserved := true
	var continuation responsesWebSocketContinuation
	for {
		if payload == nil {
			var message webSocketClientMessage
			select {
			case <-ctx.Done():
				return
			case message = <-messages:
			}
			if message.kind != websocket.TextMessage {
				closeWithWebSocketError(conn, http.StatusBadRequest, "invalid_message_type", "only text messages are supported")
				return
			}
			payload = message.body
			payloadReserved = message.reserved
		}

		if ctx.Err() != nil {
			return
		}
		var control struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(payload, &control) == nil && (control.Type == "response.cancel" || control.Type == "response.interrupt") {
			payload = nil
			_ = writeWebSocketError(conn, http.StatusBadRequest, "response_not_active", "the requested response is not active")
			continue
		}
		requestBody, warmup, err := normalizeWebSocketResponseCreatePayload(payload)
		payload = nil
		reserved := payloadReserved
		payloadReserved = false
		if err != nil {
			if reserved {
				active.discardReservation()
			}
			_ = writeWebSocketError(conn, http.StatusBadRequest, "invalid_request", err.Error())
			continue
		}
		if warmup {
			active.discardReservation()
			if err := writeWebSocketWarmupResponse(conn); err != nil {
				return
			}
			continue
		}

		requestBody, input, replayable := continuation.prepare(requestBody)
		turn, err := serveResponseCreateOverWebSocketWithControl(ctx, c, conn, bridgeRouter, requestBody, &active)
		if responseID, interrupted := active.result(); interrupted && ctx.Err() == nil {
			// The HTTP bridge cannot undo tokens already generated. Cancel upstream,
			// discard late frames, and acknowledge before accepting the next turn.
			event, _ := json.Marshal(map[string]interface{}{"type": "response.cancelled", "response": map[string]interface{}{"id": responseID, "status": "cancelled"}})
			_ = conn.SetWriteDeadline(time.Now().Add(responsesWebSocketWriteTimeout))
			if conn.WriteMessage(websocket.TextMessage, event) != nil {
				return
			}
			continue
		}
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) || isWebSocketClosed(err) {
				return
			}
			_ = writeWebSocketError(conn, http.StatusInternalServerError, "stream_error", err.Error())
			continue
		}
		continuation.record(input, replayable, turn)
	}
}

type webSocketClientMessage struct {
	kind     int
	body     []byte
	reserved bool
}

func serveResponseCreateOverWebSocket(
	ctx context.Context,
	parent fiber.Ctx,
	conn *websocket.Conn,
	httpHandler http.Handler,
	requestBody []byte,
) (*responsesWebSocketTurn, error) {
	return serveResponseCreateOverWebSocketWithControl(ctx, parent, conn, httpHandler, requestBody, nil)
}

func serveResponseCreateOverWebSocketWithControl(ctx context.Context, parent fiber.Ctx, conn *websocket.Conn, httpHandler http.Handler, requestBody []byte, active *webSocketActiveTurn) (*responsesWebSocketTurn, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if active != nil {
		active.start(cancel)
		defer active.finish()
	}
	req, err := http.NewRequestWithContext(common.WithoutUsageDrain(ctx), http.MethodPost, httpbridge.Request(parent).URL.RequestURI(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.RemoteAddr = httpbridge.Request(parent).RemoteAddr
	copyWebSocketRequestHeaders(httpbridge.Request(parent).Header, req.Header)
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(requestBody))

	writer := newResponsesWebSocketWriter(conn)
	writer.context = ctx
	writer.cancel = cancel
	writer.active = active
	httpHandler.ServeHTTP(writer, req)
	err = writer.finish()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return &writer.continuation, err
}

func copyWebSocketRequestHeaders(src http.Header, dst http.Header) {
	for key, values := range src {
		switch strings.ToLower(key) {
		case "connection", "upgrade", "sec-websocket-key", "sec-websocket-version", "sec-websocket-extensions", "sec-websocket-protocol":
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

type responsesWebSocketWriter struct {
	observeWrite func() func()
	active       *webSocketActiveTurn
	conn         *websocket.Conn
	context      context.Context
	cancel       context.CancelFunc
	header       http.Header
	status       int
	size         int
	written      bool
	writeErr     error
	mu           sync.Mutex
	sseBuffer    bytes.Buffer
	currentData  strings.Builder
	continuation responsesWebSocketTurn
}

func newResponsesWebSocketWriter(conn *websocket.Conn) *responsesWebSocketWriter {
	return &responsesWebSocketWriter{
		conn:   conn,
		header: make(http.Header),
		status: http.StatusOK,
		size:   -1,
	}
}

func (w *responsesWebSocketWriter) ObserveWrites(observe func() func()) { w.observeWrite = observe }
func (w *responsesWebSocketWriter) Header() http.Header {
	return w.header
}

func (w *responsesWebSocketWriter) WriteHeader(statusCode int) {
	if statusCode <= 0 {
		return
	}
	if w.Written() && w.status != statusCode {
		return
	}
	w.status = statusCode
	if statusCode >= http.StatusBadRequest {
		w.setWriteError(writeWebSocketError(w.conn, statusCode, "upstream_error", http.StatusText(statusCode)))
	}
}

func (w *responsesWebSocketWriter) WriteHeaderNow() {
	w.written = true
	if w.size < 0 {
		w.size = 0
	}
}

func (w *responsesWebSocketWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.writeErr != nil {
		return 0, w.writeErr
	}
	w.WriteHeaderNow()
	w.size += len(data)
	if w.status >= http.StatusBadRequest {
		return len(data), nil
	}

	w.sseBuffer.Write(data)
	w.flushSSEFrames(false)
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return len(data), nil
}

func (w *responsesWebSocketWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *responsesWebSocketWriter) Status() int {
	return w.status
}

func (w *responsesWebSocketWriter) Size() int {
	return w.size
}

func (w *responsesWebSocketWriter) Written() bool {
	return w.written
}

func (w *responsesWebSocketWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.WriteHeaderNow()
	w.flushSSEFrames(false)
}

func (w *responsesWebSocketWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("websocket response writer does not support hijack")
}

func (w *responsesWebSocketWriter) CloseNotify() <-chan bool {
	ch := make(chan bool)
	return ch
}

func (w *responsesWebSocketWriter) Pusher() http.Pusher {
	return nil
}

func (w *responsesWebSocketWriter) finish() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushSSEFrames(true)
	return w.writeErr
}

func (w *responsesWebSocketWriter) flushSSEFrames(final bool) {
	for {
		line, ok := takeSSELine(&w.sseBuffer)
		if !ok {
			break
		}
		w.processSSELine(line)
		if w.writeErr != nil {
			return
		}
	}
	if final && w.sseBuffer.Len() > 0 {
		w.processSSELine(w.sseBuffer.String())
		w.sseBuffer.Reset()
	}
	if final && strings.TrimSpace(w.currentData.String()) != "" {
		w.sendCurrentData()
	}
}

func (w *responsesWebSocketWriter) processSSELine(line string) {
	line = strings.TrimRight(line, "\r")
	if line == "" {
		w.sendCurrentData()
		return
	}
	if strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
		return
	}
	if !strings.HasPrefix(line, "data:") {
		return
	}

	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "" || data == "[DONE]" {
		return
	}
	if w.currentData.Len() > 0 {
		w.currentData.WriteByte('\n')
	}
	w.currentData.WriteString(data)
}

func (w *responsesWebSocketWriter) sendCurrentData() {
	data := strings.TrimSpace(w.currentData.String())
	w.currentData.Reset()
	if data == "" || w.writeErr != nil {
		return
	}

	if w.context != nil && w.context.Err() != nil {
		w.writeErr = w.context.Err()
		return
	}
	if w.active != nil {
		w.active.observe(data)
	}
	_ = w.conn.SetWriteDeadline(time.Now().Add(responsesWebSocketWriteTimeout))
	payload := redaction.ErrorJSON([]byte(data))
	endWrite := func() {}
	if w.observeWrite != nil {
		endWrite = w.observeWrite()
	}
	w.setWriteError(w.conn.WriteMessage(websocket.TextMessage, payload))
	endWrite()
	if w.writeErr == nil {
		w.continuation.observe(data)
	}
}

// Delivery failure is a client cancellation, not an upstream/key failure.
func (w *responsesWebSocketWriter) setWriteError(err error) {
	if err != nil && w.cancel != nil {
		w.cancel()
		err = w.context.Err()
	}
	w.writeErr = err
}

func takeSSELine(buf *bytes.Buffer) (string, bool) {
	data := buf.Bytes()
	for i, b := range data {
		if b == '\n' {
			lineBytes := make([]byte, i)
			copy(lineBytes, data[:i])
			buf.Next(i + 1)
			return string(lineBytes), true
		}
	}
	return "", false
}

func writeWebSocketError(conn *websocket.Conn, status int, code, message string) error {
	payload, err := json.Marshal(fiber.Map{
		"type":        "error",
		"status_code": status,
		"error": fiber.Map{
			"code":    code,
			"message": redaction.String(message),
		},
	})
	if err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(responsesWebSocketWriteTimeout))
	return conn.WriteMessage(websocket.TextMessage, payload)
}

func writeWebSocketWarmupResponse(conn *websocket.Conn) error {
	created := fiber.Map{
		"type": "response.created",
		"response": fiber.Map{
			"id":     "",
			"status": "in_progress",
			"output": []interface{}{},
		},
	}
	completed := fiber.Map{
		"type": "response.completed",
		"response": fiber.Map{
			"id":       "",
			"status":   "completed",
			"output":   []interface{}{},
			"end_turn": false,
			"usage": fiber.Map{
				"input_tokens":  0,
				"output_tokens": 0,
				"total_tokens":  0,
			},
		},
	}
	for _, event := range []fiber.Map{created, completed} {
		payload, err := json.Marshal(event)
		if err != nil {
			return err
		}
		_ = conn.SetWriteDeadline(time.Now().Add(responsesWebSocketWriteTimeout))
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			return err
		}
	}
	return nil
}

func closeWithWebSocketError(conn *websocket.Conn, status int, code, message string) {
	_ = writeWebSocketError(conn, status, code, message)
	closePayload := websocket.FormatCloseMessage(websocket.CloseUnsupportedData, safeWebSocketCloseReason(message))
	_ = conn.WriteControl(websocket.CloseMessage, closePayload, time.Now().Add(responsesWebSocketWriteTimeout))
}

func isWebSocketClosed(err error) bool {
	return websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) ||
		errors.Is(err, net.ErrClosed)
}

// webSocketActiveTurn has no connection writes: the reader may only cancel the
// current worker. All data frames remain serialized through that worker.
type webSocketActiveTurn struct {
	mu                sync.Mutex
	cancel            context.CancelFunc
	responseID        string
	interrupted       bool
	queued            int
	queuedInterrupted bool
}

func (a *webSocketActiveTurn) start(cancel context.CancelFunc) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cancel = cancel
	a.responseID = ""
	a.interrupted = a.queued > 0 && a.queuedInterrupted
	if a.queued > 0 {
		a.queued--
	}
	a.queuedInterrupted = false
	if a.interrupted {
		cancel()
	}
}
func (a *webSocketActiveTurn) finish() { a.mu.Lock(); defer a.mu.Unlock(); a.cancel = nil }
func (a *webSocketActiveTurn) observe(data string) {
	var event struct {
		Type     string `json:"type"`
		Response struct {
			ID string `json:"id"`
		} `json:"response"`
	}
	if json.Unmarshal([]byte(data), &event) != nil || event.Response.ID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.responseID = event.Response.ID
	switch event.Type {
	case "response.completed", "response.failed", "response.incomplete":
		a.cancel = nil
	}
}
func (a *webSocketActiveTurn) interrupt(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel == nil && id == "" && a.queued > 0 {
		a.queuedInterrupted = true
		return true
	}
	if a.cancel == nil || (id != "" && id != a.responseID) {
		return false
	}
	a.interrupted = true
	a.cancel()
	return true
}
func (a *webSocketActiveTurn) result() (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.responseID, a.interrupted
}

func (a *webSocketActiveTurn) reserve() { a.mu.Lock(); defer a.mu.Unlock(); a.queued++ }

func (a *webSocketActiveTurn) discardReservation() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.queued > 0 {
		a.queued--
	}
	a.queuedInterrupted = false
}

func safeWebSocketCloseReason(message string) string {
	message = redaction.String(message)
	if len(message) > 123 {
		message = message[:123]
		for !utf8.ValidString(message) {
			message = message[:len(message)-1]
		}
	}
	return message
}
