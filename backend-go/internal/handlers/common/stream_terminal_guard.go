package common

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/converters"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/utils"
	"io"
	"net/http"
	"strings"
	"sync"
)

const unconfirmedTerminalTailLimit = 256 << 10
const terminalParserBufferLimit = 4 << 20

type upstreamTerminalBody struct {
	io.ReadCloser
	mu          sync.Mutex
	protocol    string
	remainder   string
	data        []string
	event       string
	terminal    bool
	failed      bool
	dataBytes   int
	parseErr    error
	lastPayload string
}

func (r *upstreamTerminalBody) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.mu.Lock()
	for offset := 0; offset < n && r.parseErr == nil; {
		end := offset + (32 << 10)
		if end > n {
			end = n
		}
		r.remainder += string(p[offset:end])
		offset = end
		for {
			index := strings.IndexByte(r.remainder, '\n')
			if index < 0 {
				break
			}
			if index > terminalParserBufferLimit {
				r.parseErr = fmt.Errorf("%w: upstream_frame_limit", ErrStreamUnexpectedEOF)
				break
			}
			r.line(r.remainder[:index])
			r.remainder = r.remainder[index+1:]
			if r.parseErr != nil {
				break
			}
		}
		if len(r.remainder) > terminalParserBufferLimit {
			r.parseErr = fmt.Errorf("%w: upstream_frame_limit", ErrStreamUnexpectedEOF)
		}
	}
	if r.parseErr != nil {
		r.remainder = ""
	}

	if err != nil {
		if r.remainder != "" {
			r.line(r.remainder)
			r.remainder = ""
		}
		r.line("")
	}
	parseErr := r.parseErr
	r.mu.Unlock()
	if parseErr != nil {
		return 0, parseErr
	}
	return n, err
}
func (r *upstreamTerminalBody) line(line string) {
	if r.parseErr != nil {
		return
	}
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		payload := strings.Join(r.data, "\n")
		if len(r.data) == 0 {
			payload = r.lastPayload
		}
		terminal, failed := streamFrameTerminal(payload, r.event, r.protocol, true)
		r.terminal = r.terminal || terminal
		r.failed = r.failed || failed
		r.data = nil
		r.dataBytes = 0
		r.lastPayload = ""
		r.event = ""
		return
	}
	if strings.HasPrefix(line, "event:") {
		r.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
	}
	if strings.HasPrefix(line, "data:") {
		payload := strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
		// Some Chat providers use JSON-per-line without blank separators.
		if len(r.data) == 0 && (json.Valid([]byte(payload)) || strings.TrimSpace(payload) == "[DONE]") {
			terminal, failed := streamFrameTerminal(payload, r.event, r.protocol, true)
			r.terminal = r.terminal || terminal
			r.failed = r.failed || failed
			r.lastPayload = payload
			return
		}
		r.dataBytes += len(line) + 1
		if r.dataBytes > terminalParserBufferLimit {
			r.parseErr = fmt.Errorf("%w: upstream_frame_limit", ErrStreamUnexpectedEOF)
			r.data = nil
			r.dataBytes = 0
			return
		}
		r.data = append(r.data, payload)
	}
}
func (r *upstreamTerminalBody) state() (bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.terminal, r.failed
}

type streamTerminalWriter struct {
	httpbridge.ResponseWriter
	source        *upstreamTerminalBody
	clientContext context.Context
	protocol      string
	buffer        bytes.Buffer
	pending       bytes.Buffer
	terminal      bool
	failed        bool
	rejected      error
	doneSent      bool
	lastPayload   string
}

func (w *streamTerminalWriter) Write(data []byte) (int, error) {
	if w.clientContext != nil && w.clientContext.Err() != nil {
		return 0, w.clientContext.Err()
	}
	if w.rejected != nil {
		return len(data), nil
	}
	for offset := 0; offset < len(data); {
		endOffset := offset + (32 << 10)
		if endOffset > len(data) {
			endOffset = len(data)
		}
		w.buffer.Write(data[offset:endOffset])
		offset = endOffset
		for {
			raw := w.buffer.Bytes()
			end := bytes.Index(raw, []byte("\n\n"))
			width := 2
			if crlf := bytes.Index(raw, []byte("\r\n\r\n")); crlf >= 0 && (end < 0 || crlf < end) {
				end = crlf
				width = 4
			}
			if end < 0 {
				// Flush complete JSON-per-line data without waiting for a blank separator.
				if newline := bytes.LastIndexByte(raw, '\n'); newline >= 0 {
					payload, _ := streamFrameData(raw[:newline+1])
					if streamPayloadValid(payload) {
						end = newline
						width = 1
					}
				}
			}
			if end < 0 {
				break
			}
			if end+width > terminalParserBufferLimit {
				return 0, w.reject("upstream_frame_limit")
			}
			frame := append([]byte(nil), w.buffer.Next(end+width)...)
			if err := w.frame(frame); err != nil {
				return 0, err
			}
		}
		if w.buffer.Len() > terminalParserBufferLimit {
			return 0, w.reject("upstream_frame_limit")
		}
	}

	return len(data), nil
}
func (w *streamTerminalWriter) Unwrap() http.ResponseWriter          { return w.ResponseWriter }
func (w *streamTerminalWriter) WriteString(data string) (int, error) { return w.Write([]byte(data)) }
func (w *streamTerminalWriter) Flush() {
	if w.clientContext == nil || w.clientContext.Err() == nil {
		if w.buffer.Len() > 0 && strings.Trim(string(w.buffer.Bytes()), "\r\n") == "" {
			frame := append([]byte(nil), w.buffer.Bytes()...)
			w.buffer.Reset()
			if err := w.frame(frame); err != nil {
				w.rejected = err
				return
			}
		}
		w.ResponseWriter.Flush()
	}
}
func (w *streamTerminalWriter) frame(frame []byte) error {
	data, event := streamFrameData(frame)
	if data == "" && event != "" {
		data = w.lastPayload
	}
	if bytes.HasSuffix(frame, []byte("\n\n")) || bytes.HasSuffix(frame, []byte("\r\n\r\n")) || strings.Trim(string(frame), "\r\n") == "" {
		w.lastPayload = ""
	} else if data != "" {
		w.lastPayload = data
	}
	terminal, failed := streamFrameTerminal(data, event, w.protocol, false)
	sourceTerminal, sourceFailed := w.source.state()
	if failed {
		w.pending.Reset()
		w.failed = true
		w.terminal = true
		_, err := w.ResponseWriter.Write(frame)
		return err
	}
	isDone := w.protocol == "chat" && strings.TrimSpace(data) == "[DONE]"
	if isDone && w.doneSent {
		return nil
	}
	if isDone {
		w.doneSent = true
	}
	if w.failed && terminal {
		if isDone {
			_, err := w.ResponseWriter.Write(frame)
			return err
		}
		return nil
	}
	if w.terminal && terminal && !(w.protocol == "chat" && strings.TrimSpace(data) == "[DONE]") {
		return nil
	}
	if sourceFailed && terminal {
		return w.reject("upstream_failed")
	}
	if terminal {
		w.terminal = true
	}
	if (!sourceTerminal && terminal) || w.pending.Len() > 0 {
		w.pending.Write(frame)
		if w.pending.Len() > unconfirmedTerminalTailLimit {
			_ = w.source.Close()
			return w.reject("unconfirmed_terminal")
		}
		if !sourceTerminal {
			return nil
		}
		_, err := w.ResponseWriter.Write(w.pending.Bytes())
		w.pending.Reset()
		w.terminal = w.terminal || terminal
		return err
	}
	if terminal {
		w.terminal = true
	}
	_, err := w.ResponseWriter.Write(frame)
	return err
}
func (w *streamTerminalWriter) finish() error {
	if w.rejected != nil {
		return w.rejected
	}
	if w.buffer.Len() > 0 {
		frame := append([]byte(nil), w.buffer.Bytes()...)
		w.buffer.Reset()
		data, _ := streamFrameData(frame)
		if strings.TrimSpace(data) != "" && strings.TrimSpace(data) != "[DONE]" && !streamPayloadValid(data) {
			return w.reject("upstream_eof")
		}
		if err := w.frame(frame); err != nil {
			return err
		}
	}
	terminal, failed := w.source.state()
	w.source.mu.Lock()
	parseErr := w.source.parseErr
	w.source.mu.Unlock()
	if parseErr != nil {
		return w.reject("upstream_frame_limit")
	}
	if !terminal {
		return w.reject("upstream_eof")
	}
	if failed {
		if !w.failed {
			return w.reject("upstream_failed")
		}
		return fmt.Errorf("upstream stream failed")
	}
	if w.pending.Len() > 0 {
		_, err := w.ResponseWriter.Write(w.pending.Bytes())
		w.pending.Reset()
		if err != nil {
			return err
		}
	}
	return nil
}
func (w *streamTerminalWriter) reject(reason string) error {
	if w.rejected != nil {
		return w.rejected
	}
	w.pending.Reset()
	w.buffer.Reset()
	w.rejected = fmt.Errorf("%w: %s", ErrStreamUnexpectedEOF, reason)
	if !w.ResponseWriter.Written() {
		w.rejected = fmt.Errorf("%w: %w", ErrEmptyStreamResponse, w.rejected)
	}
	if !w.failed && w.ResponseWriter.Written() {
		var event string
		switch w.protocol {
		case "responses":
			event = converters.SynthesizeResponsesIncomplete(0, "", reason)
		case "messages":
			event = BuildStreamErrorEvent(w.rejected)
		case "chat":
			event = "data: {\"error\":{\"type\":\"upstream_stream_error\",\"code\":\"" + reason + "\",\"message\":\"upstream stream ended before protocol completion\",\"source\":\"gateway\"}}\n\ndata: [DONE]\n\n"
		case "gemini":
			event = "data: {\"error\":{\"code\":502,\"status\":\"INTERNAL\",\"message\":\"upstream stream ended before protocol completion\",\"source\":\"gateway\"}}\n\n"
		}
		_, _ = w.ResponseWriter.Write([]byte(event))
		w.ResponseWriter.Flush()
		w.failed = true
	}
	return w.rejected
}

func streamFrameData(frame []byte) (string, string) {
	var data []string
	event := ""
	for _, line := range strings.Split(string(frame), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	return strings.Join(data, "\n"), event
}
func streamFrameTerminal(payload, event, protocol string, source bool) (bool, bool) {
	if strings.TrimSpace(payload) == "[DONE]" {
		return protocol == "chat", false
	}
	var object map[string]interface{}
	if json.Unmarshal([]byte(payload), &object) != nil {
		// Preserve the accepted JSON-per-line framing as well as standard SSE.
		if strings.Contains(payload, "\n") {
			terminal, failed := false, false
			for _, line := range strings.Split(payload, "\n") {
				t, f := streamFrameTerminal(line, event, protocol, source)
				terminal = terminal || t
				failed = failed || f
			}
			return terminal, failed
		}
		return false, false
	}
	if failure, ok := object["error"].(map[string]interface{}); ok && len(failure) > 0 {
		return true, true
	}
	kind, _ := object["type"].(string)
	if kind == "" {
		kind = event
	}
	if kind == "error" || kind == "response.error" || kind == "response.failed" {
		return true, true
	}
	switch protocol {
	case "messages":
		return kind == "message_stop", false
	case "responses":
		if response, ok := object["response"].(map[string]interface{}); ok {
			if failure, ok := response["error"].(map[string]interface{}); ok && len(failure) > 0 {
				return true, true
			}
			if response["status"] == "failed" {
				return true, true
			}
		}
		if kind == "response.incomplete" {
			return true, !source
		}
		return kind == "response.completed", false
	case "chat":
		choices, _ := object["choices"].([]interface{})
		for _, value := range choices {
			if choice, ok := value.(map[string]interface{}); ok {
				if reason, ok := choice["finish_reason"].(string); ok && reason != "" {
					return true, false
				}
			}
		}
	case "gemini":
		if feedback, ok := object["promptFeedback"].(map[string]interface{}); ok {
			if reason, ok := feedback["blockReason"].(string); ok && types.GeminiPromptBlocked(reason) {
				return true, false
			}
		}
		candidates, _ := object["candidates"].([]interface{})
		for _, value := range candidates {
			if candidate, ok := value.(map[string]interface{}); ok {
				if value, present := candidate["finishReason"]; present && value != nil {
					reason, err := types.ParseGeminiFinishReason(value)
					if err != nil {
						return true, true
					}
					if types.GeminiFinishReasonTerminal(reason) {
						return true, types.GeminiFinishReasonFailed(reason)
					}
				}
			}
		}
	}
	return false, false
}
func terminalProtocol(value string) string {
	switch strings.ToLower(value) {
	case "messages", "claude":
		return "messages"
	case "openai", "chat":
		return "chat"
	case "responses", "copilot":
		return "responses"
	case "gemini":
		return "gemini"
	default:
		return ""
	}
}

func withStreamTerminalGuard(c fiber.Ctx, resp *http.Response, sourceProtocol, outlet string, run func() (*types.Usage, error)) (*types.Usage, error) {
	sourceProtocol = terminalProtocol(sourceProtocol)
	outlet = terminalProtocol(outlet)
	if sourceProtocol == "" || outlet == "" {
		return run()
	}
	if err := utils.DecompressResponseBodyIfNeeded(resp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponseBody, err)
	}
	source := &upstreamTerminalBody{ReadCloser: resp.Body, protocol: sourceProtocol}
	resp.Body = source
	original := httpbridge.Writer(c)
	guarded := &streamTerminalWriter{ResponseWriter: original, source: source, protocol: outlet, clientContext: httpbridge.Request(c).Context()}
	httpbridge.SetWriter(c, guarded)
	defer httpbridge.SetWriter(c, original)
	usage, err := run()
	if err != nil {
		return usage, err
	}
	if canceled := httpbridge.Request(c).Context().Err(); canceled != nil {
		return nil, canceled
	}
	if err := guarded.finish(); err != nil {
		return nil, err
	}
	return usage, nil
}

func streamPayloadValid(payload string) bool {
	if json.Valid([]byte(payload)) || strings.TrimSpace(payload) == "[DONE]" {
		return true
	}
	for _, line := range strings.Split(payload, "\n") {
		if strings.TrimSpace(line) != "" && !json.Valid([]byte(line)) && strings.TrimSpace(line) != "[DONE]" {
			return false
		}
	}
	return strings.TrimSpace(payload) != ""
}
