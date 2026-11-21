package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/copilot"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/providers"
	"github.com/soulteary/yunqi/internal/types"
)

func TestCopilotModelEndpointsThroughResponsesHandler(t *testing.T) {
	for _, tc := range []struct {
		name, metadata, preferred, path, wire string
		reject                                bool
	}{
		{"responses", `["/responses"]`, "", "/responses", "responses", false},
		{"chat", `["/chat/completions"]`, "", "/chat/completions", "openai", false},
		{"messages", `["/v1/messages"]`, "", "/v1/messages", "claude", false},
		{"mixed", `["ws:/responses","/chat/completions","/responses"]`, "", "/responses", "responses", false},
		{"explicit_route", `["/responses","/v1/messages"]`, "messages", "/v1/messages", "claude", false},
		{"legacy", "", "", "/responses", "responses", false},
		{"ws_only", `["ws:/responses"]`, "", "", "", true},
		{"url_injection", `["https://example.invalid/responses"]`, "", "", "", true},
		{"empty", `[]`, "", "", "", true},
		{"unsupported_route", `["/responses"]`, "chat", "", "", true},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer runtime-fake" || r.Header.Get("Copilot-Integration-Id") == "" || r.Header.Get("X-Api-Key") != "" {
						t.Errorf("authentication identity not preserved: %v", r.Header)
					}
					if r.URL.Path == "/models" {
						w.Header().Set("Content-Type", "application/json")
						entry := `{"id":"opaque-model"}`
						if tc.metadata != "" {
							entry = `{"id":"opaque-model","supported_endpoints":` + tc.metadata + `}`
						}
						fmt.Fprint(w, `{"data":[`+entry+`]}`)
						return
					}
					calls.Add(1)
					if r.URL.Path != tc.path {
						t.Errorf("wrong endpoint %s, want %s", r.URL.Path, tc.path)
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					key := "input"
					if tc.wire != "responses" {
						key = "messages"
					}
					if body[key] == nil || body["model"] != "opaque-model" {
						t.Errorf("wrong %s body: %v", tc.wire, body)
					}
					jsonBody, sseBody := copilotEndpointFixture(tc.wire)
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, sseBody)
					} else {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, jsonBody)
					}
				}))
				defer server.Close()
				resolver := copilot.NewEndpointResolver(func(_ context.Context, token, proxy string) (string, string, error) {
					if token != "github-fake" {
						t.Errorf("wrong OAuth credential %q", token)
					}
					return "runtime-fake", server.URL, nil
				}, server.Client())
				provider := &providers.ResponsesProvider{CopilotResolver: resolver}
				upstream := &config.UpstreamConfig{ServiceType: "copilot", CopilotProtocol: tc.preferred, BaseURL: "https://unused.invalid", CustomHeaders: map[string]string{"Authorization": "bad", "X-Api-Key": "bad", "User-Agent": "bad"}}
				raw := []byte(fmt.Sprintf(`{"model":"opaque-model","input":"hello","store":false,"stream":%t}`, stream))
				writer := httptest.NewRecorder()
				ctx, _ := httpbridge.TestContext(writer)
				httpbridge.SetRequest(ctx, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(raw))))
				req, _, err := provider.ConvertBodyToProviderRequest(ctx, upstream, "github-fake", raw, "/v1/responses")
				if tc.reject {
					if err == nil {
						t.Fatal("explicit unsupported endpoint accepted")
					}
					if calls.Load() != 0 {
						t.Fatal("unsupported endpoint was sent")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if upstream.ServiceType != "copilot" {
					t.Fatal("OAuth identity mutated")
				}
				if got := providers.ActualServiceType(ctx, "copilot"); got != tc.wire {
					t.Fatalf("wire evidence=%s", got)
				}
				resp, err := server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				var original types.ResponsesRequest
				if err := json.Unmarshal(raw, &original); err != nil {
					t.Fatal(err)
				}
				usage, err := handleSuccess(ctx, resp, provider, upstream, "github-fake", "copilot", &config.EnvConfig{}, nil, time.Now(), &original, raw, common.StreamPreflightTimeouts{})
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(writer.Body.String(), "hello") {
					t.Fatalf("response lost: %s", writer.Body.String())
				}
				if usage == nil || usage.InputTokens != 10 || usage.OutputTokens != 3 {
					t.Fatalf("usage=%+v body=%s", usage, writer.Body.String())
				}
				if stream && strings.Count(writer.Body.String(), "event: response.completed\n") != 1 {
					t.Fatalf("terminal response duplicated or missing: %s", writer.Body.String())
				}
			})
		}
	}
}

func copilotEndpointFixture(wire string) (string, string) {
	switch wire {
	case "openai":
		body := `{"id":"chat-1","model":"opaque-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`
		sse := "data: {\"id\":\"chat-1\",\"model\":\"opaque-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"chat-1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":3,\"total_tokens\":13}}\n\ndata: [DONE]\n\n"
		return body, sse
	case "claude":
		body := `{"id":"msg-1","type":"message","role":"assistant","model":"opaque-model","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":3}}`
		sse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-1\",\"model\":\"opaque-model\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
		return body, sse
	default:
		body := `{"id":"resp-1","model":"opaque-model","status":"completed","output":[{"id":"msg-1","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}}`
		return body, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + body + "}\n\n"
	}
}

// Messages ingress uses a provider delegate; its stream parser and the nonstream
// SSE bridge must consume the selected wire, rather than the OAuth service name.
func TestCopilotMessagesIngressStreamAndNonStreamBridge(t *testing.T) {
	for _, wire := range []string{"responses", "openai", "claude"} {
		for _, clientStream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", wire, clientStream), func(t *testing.T) {
				path := map[string]string{"responses": "/responses", "openai": "/chat/completions", "claude": "/v1/messages"}[wire]
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/models" {
						fmt.Fprintf(w, `{"data":[{"id":"opaque-model","supported_endpoints":[%q]}]}`, path)
						return
					}
					if r.URL.Path != path {
						t.Errorf("wrong endpoint %s", r.URL.Path)
					}
					_, sse := copilotEndpointFixture(wire)
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, sse)
				}))
				defer server.Close()
				resolver := copilot.NewEndpointResolver(func(context.Context, string, string) (string, string, error) { return "runtime-fake", server.URL, nil }, server.Client())
				provider := &providers.ResponsesProvider{CopilotResolver: resolver}
				raw := []byte(fmt.Sprintf(`{"model":"opaque-model","messages":[{"role":"user","content":"hello"}],"max_tokens":20,"stream":%t}`, clientStream))
				ctx, _ := httpbridge.TestContext(httptest.NewRecorder())
				httpbridge.SetRequest(ctx, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(raw))))
				req, _, err := provider.ConvertBodyToProviderRequest(ctx, &config.UpstreamConfig{ServiceType: "copilot"}, "github-fake", raw, "/v1/messages")
				if err != nil {
					t.Fatal(err)
				}
				resp, err := server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				if clientStream {
					events, errors, err := provider.HandleStreamResponse(resp.Body)
					if err != nil {
						t.Fatal(err)
					}
					output := ""
					for event := range events {
						output += event
					}
					select {
					case err := <-errors:
						if err != nil {
							t.Fatal(err)
						}
					default:
					}
					if !strings.Contains(output, "hello") || !strings.Contains(output, "message_stop") {
						t.Fatalf("Messages SSE lost: %s", output)
					}
				} else {
					body, err := common.ReadUpstreamNonStreamBody(ctx, resp, providers.ActualServiceType(ctx, "copilot"))
					resp.Body.Close()
					if err != nil {
						t.Fatal(err)
					}
					converted, err := provider.ConvertToClaudeResponse(&types.ProviderResponse{Body: body})
					if err != nil {
						t.Fatal(err)
					}
					encoded, _ := json.Marshal(converted)
					if !strings.Contains(string(encoded), "hello") || converted.Usage.InputTokens != 10 || converted.Usage.OutputTokens != 3 {
						t.Fatalf("Messages bridge lost: %s", encoded)
					}
				}
			})
		}
	}
}
