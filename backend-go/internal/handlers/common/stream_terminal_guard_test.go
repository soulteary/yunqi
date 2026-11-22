package common

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/warmup"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var terminalGuardPrefixes = map[string]string{
	"messages":  "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n",
	"responses": "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n",
	"chat":      "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n",
	"gemini":    "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\n",
}
var terminalGuardEnds = map[string]string{
	"messages":  "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	"responses": "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n",
	"chat":      "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
	"gemini":    "data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n",
}

func TestStreamTerminalGuardEverySourceAndOutlet(t *testing.T) {
	for source, prefix := range terminalGuardPrefixes {
		for outlet, end := range terminalGuardEnds {
			for _, complete := range []bool{false, true} {
				name := source + "_to_" + outlet
				if complete {
					name += "_complete"
				}
				t.Run(name, func(t *testing.T) {
					raw := prefix
					if complete {
						raw += terminalGuardEnds[source]
					}
					resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}
					w := httptest.NewRecorder()
					c, _ := httpbridge.TestContext(w)
					httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/test", nil))
					expected := terminalGuardPrefixes[outlet] + end
					_, err := withStreamTerminalGuard(c, resp, source, outlet, func() (*types.Usage, error) {
						_, _ = io.ReadAll(resp.Body)
						_, _ = httpbridge.Writer(c).Write([]byte(expected))
						httpbridge.Writer(c).Flush()
						return &types.Usage{}, nil
					})
					if complete {
						if err != nil {
							t.Fatal(err)
						}
						if w.Body.String() != expected {
							t.Fatalf("normal bytes changed: %q", w.Body.String())
						}
						return
					}
					if !errors.Is(err, ErrStreamUnexpectedEOF) {
						t.Fatalf("partial stream succeeded: %v %s", err, w.Body.String())
					}
					forbidden := map[string]string{"messages": "event: message_stop", "responses": `"type":"response.completed"`, "chat": `"finish_reason":"stop"`, "gemini": `"finishReason":"STOP"`}[outlet]
					if strings.Contains(w.Body.String(), forbidden) || !strings.Contains(w.Body.String(), "error") && !strings.Contains(w.Body.String(), "response.incomplete") {
						t.Fatalf("wrong error outlet %s", w.Body.String())
					}
				})
			}
		}
	}
}

func TestStreamTerminalGuardWaitsForSourceStopWithoutDelayingContent(t *testing.T) {
	prefix := terminalGuardPrefixes["messages"]
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(prefix + terminalGuardEnds["messages"]))}
	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	_, err := withStreamTerminalGuard(c, resp, "messages", "chat", func() (*types.Usage, error) {
		first := make([]byte, len(prefix))
		if _, err := io.ReadFull(resp.Body, first); err != nil {
			t.Fatal(err)
		}
		_, _ = httpbridge.Writer(c).Write([]byte(terminalGuardPrefixes["chat"]))
		httpbridge.Writer(c).Flush()
		if !strings.Contains(w.Body.String(), "hello") {
			t.Fatal("content buffered until whole response")
		}
		_, _ = httpbridge.Writer(c).Write([]byte(terminalGuardEnds["chat"]))
		httpbridge.Writer(c).Flush()
		if strings.Contains(w.Body.String(), "finish_reason") {
			t.Fatal("unconfirmed success already sent")
		}
		_, _ = io.ReadAll(resp.Body)
		return &types.Usage{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Body.String() != terminalGuardPrefixes["chat"]+terminalGuardEnds["chat"] {
		t.Fatalf("final bytes %s", w.Body.String())
	}
}

func TestStreamTerminalGuardHandlesGzipAndUnspecifiedPlaceholder(t *testing.T) {
	for _, complete := range []bool{false, true} {
		raw := terminalGuardPrefixes["gemini"] + "data: {\"candidates\":[{\"finishReason\":\"FINISH_REASON_UNSPECIFIED\"}]}\n\n"
		if complete {
			raw += terminalGuardEnds["gemini"]
		}
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		_, _ = writer.Write([]byte(raw))
		_ = writer.Close()
		resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Encoding": []string{"gzip"}}, Body: io.NopCloser(bytes.NewReader(compressed.Bytes()))}
		w := httptest.NewRecorder()
		c, _ := httpbridge.TestContext(w)
		httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
		_, err := withStreamTerminalGuard(c, resp, "gemini", "responses", func() (*types.Usage, error) {
			_, _ = io.ReadAll(resp.Body)
			_, _ = httpbridge.Writer(c).Write([]byte(terminalGuardPrefixes["responses"] + terminalGuardEnds["responses"]))
			return nil, nil
		})
		if complete && err != nil || !complete && !errors.Is(err, ErrStreamUnexpectedEOF) {
			t.Fatalf("complete=%v err=%v body=%s", complete, err, w.Body.String())
		}
	}
}

func TestStreamTerminalGuardNeverWritesAfterClientCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(terminalGuardPrefixes["chat"]))}
	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx))
	_, err := withStreamTerminalGuard(c, resp, "chat", "chat", func() (*types.Usage, error) {
		cancel()
		_, writeErr := httpbridge.Writer(c).Write([]byte(terminalGuardPrefixes["chat"]))
		if !errors.Is(writeErr, context.Canceled) {
			t.Fatalf("write err %v", writeErr)
		}
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) || w.Body.Len() != 0 {
		t.Fatalf("post-disconnect write %v %s", err, w.Body.String())
	}
}

func TestStreamTerminalGuardSupportsJSONLinesAndMultilineSSE(t *testing.T) {
	for _, raw := range []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\ndata: {\"choices\":[{\"finish_reason\":\"stop\"}]}\ndata: [DONE]\n",
		"data: {\"choices\": [\ndata: {\"finish_reason\":\"stop\"}]}\n\n",
	} {
		resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}
		w := httptest.NewRecorder()
		c, _ := httpbridge.TestContext(w)
		httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
		_, err := withStreamTerminalGuard(c, resp, "chat", "chat", func() (*types.Usage, error) {
			body, e := io.ReadAll(resp.Body)
			if e != nil {
				return nil, e
			}
			_, e = httpbridge.Writer(c).Write(body)
			return nil, e
		})
		if err != nil || w.Body.String() != raw {
			t.Fatalf("framing changed err=%v body=%q", err, w.Body.String())
		}
	}
}

func TestStreamTerminalGuardBoundsUnconfirmedTerminalTail(t *testing.T) {
	resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(terminalGuardPrefixes["messages"]))}
	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	_, err := withStreamTerminalGuard(c, resp, "messages", "chat", func() (*types.Usage, error) {
		_, _ = io.ReadAll(resp.Body)
		_, _ = httpbridge.Writer(c).Write([]byte(terminalGuardPrefixes["chat"]))
		_, _ = httpbridge.Writer(c).Write([]byte(terminalGuardEnds["chat"]))
		_, _ = httpbridge.Writer(c).Write([]byte(": " + strings.Repeat("x", unconfirmedTerminalTailLimit) + "\n\n"))
		return nil, nil
	})
	if !errors.Is(err, ErrStreamUnexpectedEOF) || strings.Contains(w.Body.String(), `"finish_reason":"stop"`) {
		t.Fatalf("unbounded or successful tail: err=%v len=%d", err, w.Body.Len())
	}
}

func TestExecutorTerminalGuardEveryOutlet(t *testing.T) {
	for outlet := range terminalGuardEnds {
		for _, complete := range []bool{false, true} {
			name := outlet
			if complete {
				name += "_complete"
			}
			t.Run(name, func(t *testing.T) {
				raw := terminalGuardPrefixes["messages"]
				if complete {
					raw += terminalGuardEnds["messages"]
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, raw)
				}))
				defer server.Close()
				cm, sched, mm, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{Name: "terminal", BaseURL: server.URL, APIKeys: []string{"test"}, Status: "active", ServiceType: "claude"})
				defer cleanup()
				up := &cm.GetConfig().Upstream[0]
				w := httptest.NewRecorder()
				c, _ := httpbridge.TestContext(w)
				httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/test", nil))
				handled, _, _, _, _, err := TryUpstreamWithAllKeys(c, config.NewEnvConfig(), cm, sched, scheduler.ChannelKindMessages, outlet, mm, up, []warmup.URLLatencyResult{{URL: server.URL}}, []byte(`{"model":"m","stream":true}`), nil, true,
					func(*config.UpstreamConfig, map[string]bool) (string, error) { return "test", nil }, func(fiber.Ctx, *config.UpstreamConfig, string) (*http.Request, error) {
						return http.NewRequest(http.MethodPost, server.URL+"/v1/messages", strings.NewReader(`{"model":"m","stream":true}`))
					}, func(string) {}, func(string) {}, func(string) {},
					func(c fiber.Ctx, resp *http.Response, _ *config.UpstreamConfig, _ string, _ []byte) (*types.Usage, error) {
						_, _ = io.ReadAll(resp.Body)
						_, e := httpbridge.Writer(c).Write([]byte(terminalGuardPrefixes[outlet] + terminalGuardEnds[outlet]))
						return nil, e
					}, "m", "", 0, nil)
				if complete {
					if !handled || err != nil {
						t.Fatalf("normal wiring handled=%v err=%v", handled, err)
					}
				} else {
					if !errors.Is(err, ErrStreamUnexpectedEOF) {
						t.Fatalf("executor bypassed terminal guard: handled=%v err=%v body=%s", handled, err, w.Body.String())
					}
				}
			})
		}
	}
}

func TestTerminalParserJSONLinesStateRemainsBounded(t *testing.T) {
	line := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n"
	source := &upstreamTerminalBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat(line, 50000) + "data: [DONE]\n")), protocol: "chat"}
	buffer := make([]byte, 4096)
	for {
		_, err := source.Read(buffer)
		if len(source.data) != 0 || source.dataBytes != 0 || len(source.remainder) > len(line) {
			t.Fatalf("JSON-lines retained stream: data=%d bytes=%d tail=%d", len(source.data), source.dataBytes, len(source.remainder))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if terminal, _ := source.state(); !terminal {
		t.Fatal("terminal missing")
	}
	writer := &streamTerminalWriter{ResponseWriter: newBranchRecorder(t), source: source, protocol: "chat"}
	for i := 0; i < 50000; i++ {
		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
		if writer.buffer.Len() > len(line) {
			t.Fatal("writer retained JSON-lines stream")
		}
	}
}

func newBranchRecorder(t *testing.T) httpbridge.ResponseWriter {
	t.Helper()
	c, _ := httpbridge.TestContext(httptest.NewRecorder())
	return httpbridge.Writer(c)
}

func TestTerminalParserRejectsOversizedUnterminatedFrames(t *testing.T) {
	for _, raw := range []string{"data: " + strings.Repeat("x", terminalParserBufferLimit+1), strings.Repeat("data: {\n", terminalParserBufferLimit/8+1)} {
		source := &upstreamTerminalBody{ReadCloser: io.NopCloser(strings.NewReader(raw)), protocol: "chat"}
		if _, err := io.Copy(io.Discard, source); !errors.Is(err, ErrStreamUnexpectedEOF) {
			t.Fatalf("oversized source accepted: %v", err)
		}
		if len(source.remainder) > terminalParserBufferLimit || source.dataBytes > terminalParserBufferLimit {
			t.Fatal("source limit exceeded")
		}
	}
	source := &upstreamTerminalBody{protocol: "chat"}
	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	writer := &streamTerminalWriter{ResponseWriter: httpbridge.Writer(c), source: source, protocol: "chat"}
	_, _ = writer.Write([]byte(terminalGuardPrefixes["chat"]))
	if _, err := writer.Write([]byte("data: " + strings.Repeat("x", terminalParserBufferLimit+1))); !errors.Is(err, ErrStreamUnexpectedEOF) {
		t.Fatalf("oversized writer accepted: %v", err)
	}
	if writer.buffer.Len() > terminalParserBufferLimit || strings.Contains(w.Body.String(), `"finish_reason":"stop"`) {
		t.Fatal("oversized frame produced success or retained bytes")
	}
}

func TestTerminalParserAcceptsLargeReadOfSmallFrames(t *testing.T) {
	raw := strings.Repeat(terminalGuardPrefixes["chat"], 100000) + terminalGuardEnds["chat"]
	source := &upstreamTerminalBody{ReadCloser: io.NopCloser(strings.NewReader(raw)), protocol: "chat"}
	buffer := make([]byte, len(raw)+1)
	if n, err := source.Read(buffer); err != nil || n != len(raw) {
		t.Fatalf("valid large read rejected: n=%d err=%v", n, err)
	}
	if terminal, failed := source.state(); !terminal || failed {
		t.Fatalf("terminal=%v failed=%v", terminal, failed)
	}
	if len(source.remainder) > terminalParserBufferLimit || len(source.lastPayload) > terminalParserBufferLimit {
		t.Fatal("parser retained unbounded input")
	}
}

func TestTerminalParserRecognizesEventAfterData(t *testing.T) {
	raw := "data: {\"message\":\"rejected\"}\nevent: error\n\n"
	source := &upstreamTerminalBody{ReadCloser: io.NopCloser(strings.NewReader(raw)), protocol: "responses"}
	if _, err := io.ReadAll(source); err != nil {
		t.Fatal(err)
	}
	if terminal, failed := source.state(); !terminal || !failed {
		t.Fatalf("post-data error lost: terminal=%v failed=%v", terminal, failed)
	}
}
