package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/copilot"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/types"
)

func TestCopilotExecutionRefreshesRequestEvidenceAndProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint := "/responses"
		switch r.Header.Get("Authorization") {
		case "Bearer chat":
			endpoint = "/chat/completions"
		case "Bearer messages":
			endpoint = "/v1/messages"
		case "Bearer unsupported":
			endpoint = "ws:/responses"
		}
		fmt.Fprintf(w, `{"data":[{"id":"opaque","supported_endpoints":[%q]}]}`, endpoint)
	}))
	defer server.Close()
	resolver := copilot.NewEndpointResolver(func(_ context.Context, key, proxy string) (string, string, error) { return key, server.URL, nil }, server.Client())
	provider := &ResponsesProvider{CopilotResolver: resolver}
	ctx, _ := httpbridge.TestContext(httptest.NewRecorder())
	httpbridge.SetRequest(ctx, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	for _, step := range []struct {
		key, wire string
		reject    bool
	}{{"chat", "openai", false}, {"unsupported", "", true}, {"messages", "claude", false}, {"responses", "responses", false}} {
		req, _, err := provider.ConvertBodyToProviderRequest(ctx, &config.UpstreamConfig{ServiceType: "copilot"}, step.key, []byte(`{"model":"opaque","input":"hello","store":false}`), "/v1/responses")
		if step.reject {
			if err == nil {
				t.Fatal("unsupported wire accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		req.Body.Close()
		if actual := ActualServiceType(ctx, "copilot"); actual != step.wire {
			t.Fatalf("stale wire %s want %s", actual, step.wire)
		}
		if step.wire == "responses" && provider.wireProvider != nil {
			t.Fatal("stale chat/messages delegate")
		}
	}
	// A reused provider must also reset its delegate when switching away from Copilot.
	provider.wireProvider = &OpenAIProvider{}
	req, _, err := provider.ConvertBodyToProviderRequest(ctx, &config.UpstreamConfig{ServiceType: "responses", BaseURL: server.URL}, "ordinary", []byte(`{"model":"opaque","input":"hello","store":false}`), "/v1/responses")
	if err != nil {
		t.Fatal(err)
	}
	req.Body.Close()
	if provider.wireProvider != nil || ActualServiceType(ctx, "openai") != "openai" {
		t.Fatal("Copilot state leaked into ordinary provider")
	}
}

func TestCopilotNativeChatBodyAndMessagesEntry(t *testing.T) {
	for _, wire := range []string{"responses", "chat", "messages"} {
		t.Run(wire, func(t *testing.T) {
			path := map[string]string{"responses": "/responses", "chat": "/chat/completions", "messages": "/v1/messages"}[wire]
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"data":[{"id":"opaque","supported_endpoints":[%q]}]}`, path)
			}))
			defer server.Close()
			resolver := copilot.NewEndpointResolver(func(context.Context, string, string) (string, string, error) { return "runtime", server.URL, nil }, server.Client())
			provider := &ResponsesProvider{CopilotResolver: resolver}
			ctx, _ := httpbridge.TestContext(httptest.NewRecorder())
			httpbridge.SetRequest(ctx, httptest.NewRequest(http.MethodPost, "/v1/messages", nil))
			upstream := &config.UpstreamConfig{ServiceType: "copilot"}
			message := []byte(`{"model":"opaque","messages":[{"role":"user","content":"hello"}],"max_tokens":20,"stream":true}`)
			req, _, err := provider.ConvertBodyToProviderRequest(ctx, upstream, "github-fake", message, "/v1/messages")
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(req.Body)
			req.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded["stream"] != true || req.URL.Path != path {
				t.Fatalf("wrong Messages execution: %s %s", req.URL.Path, body)
			}
			response := `{"id":"resp","model":"opaque","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":3}}`

			chatInput := []byte(`{"model":"opaque","messages":[{"role":"developer","content":"keep this instruction"},{"role":"user","content":"hello"}],"stream":true}`)
			chatReq, _, err := provider.ConvertBodyToProviderRequest(ctx, upstream, "github-fake", chatInput, "/v1/chat/completions")
			if err != nil {
				t.Fatal(err)
			}
			chatBytes, _ := io.ReadAll(chatReq.Body)
			chatReq.Body.Close()
			if !strings.Contains(string(chatBytes), "keep this instruction") {
				t.Fatalf("developer instruction lost: %s", chatBytes)
			}
			media := []byte(`{"model":"opaque","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"ZmFrZQ=="},"cache_control":{"type":"ephemeral"}},{"type":"document","source":{"type":"text","media_type":"text/plain","data":"hello"}}]}],"thinking":{"type":"enabled","budget_tokens":1024},"stop_sequences":["END"],"tool_choice":{"type":"auto"},"max_tokens":2048}`)
			mediaReq, _, mediaErr := provider.ConvertBodyToProviderRequest(ctx, upstream, "github-fake", media, "/v1/messages")
			if wire == "messages" {
				if mediaErr != nil {
					t.Fatal(mediaErr)
				}
				var originalMedia, actualMedia map[string]any
				json.Unmarshal(media, &originalMedia)
				json.NewDecoder(mediaReq.Body).Decode(&actualMedia)
				mediaReq.Body.Close()
				if !reflect.DeepEqual(originalMedia, actualMedia) {
					t.Fatalf("native Messages fields lost: %v", actualMedia)
				}
			} else if mediaErr == nil {
				t.Fatal("cross-protocol media silently accepted")
			}
			if wire == "chat" {
				response = `{"id":"chat","model":"opaque","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`
			}
			if wire == "messages" {
				response = `{"id":"msg","type":"message","role":"assistant","model":"opaque","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":10,"output_tokens":3}}`
			}
			converted, err := provider.ConvertToClaudeResponse(&types.ProviderResponse{Body: []byte(response)})
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(converted)
			if !strings.Contains(string(encoded), "hello") || converted.Usage.InputTokens != 10 {
				t.Fatalf("Messages response lost: %s", encoded)
			}
			if wire == "chat" {
				chat := []byte(`{"model":"opaque","messages":[{"role":"developer","content":"keep this instruction"},{"role":"user","content":"hello"}],"stop":["END"],"response_format":{"type":"json_object"},"stream":true,"stream_options":{"include_usage":true},"logprobs":true,"top_logprobs":3,"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":"auto"}`)
				req, _, err := provider.ConvertBodyToProviderRequest(ctx, upstream, "github-fake", chat, "/v1/chat/completions")
				if err != nil {
					t.Fatal(err)
				}
				var actual map[string]any
				if err := json.NewDecoder(req.Body).Decode(&actual); err != nil {
					t.Fatal(err)
				}
				req.Body.Close()
				if actual["logprobs"] != true || actual["top_logprobs"] != float64(3) || actual["stream"] != true || actual["stream_options"] == nil || actual["tools"] == nil || actual["tool_choice"] != "auto" || actual["stop"] == nil || actual["response_format"] == nil || !strings.Contains(fmt.Sprint(actual["messages"]), "developer") {
					t.Fatalf("native Chat fields lost: %v", actual)
				}
			}
		})
	}
}

func TestCopilotCrossWireMediaIsPreservedOrRejected(t *testing.T) {
	for _, tc := range []struct {
		name, wire, path, body string
		reject                 bool
	}{
		{"tool_input_type_is_data", "chat", "/v1/messages", `{"model":"opaque","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"lookup","input":{"type":"image","data":{"type":"file"}}}]}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`, false},
		{"function_output_type_is_data", "chat", "/v1/responses", `{"model":"opaque","input":[{"type":"function_call_output","call_id":"call-1","output":{"type":"image","data":"fake-file"}}]}`, false},
		{"chat_image_to_messages", "messages", "/v1/chat/completions", `{"model":"opaque","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/picture.png"}}]}]}`, true},
		{"responses_image_to_messages", "messages", "/v1/responses", `{"model":"opaque","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.invalid/picture.png"}]}]}`, true},
		{"chat_audio_to_responses", "responses", "/v1/chat/completions", `{"model":"opaque","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"fake-audio","format":"wav"}}]}]}`, true},
		{"chat_file_to_responses", "responses", "/v1/chat/completions", `{"model":"opaque","messages":[{"role":"user","content":[{"type":"file","file":{"file_data":"fake-file"}}]}]}`, true},
		{"chat_image_to_responses", "responses", "/v1/chat/completions", `{"model":"opaque","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/picture.png"}}]}]}`, false},
		{"responses_image_to_chat", "chat", "/v1/responses", `{"model":"opaque","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.invalid/picture.png"}]}]}`, false},
		{"native_chat_audio", "chat", "/v1/chat/completions", `{"model":"opaque","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"fake-audio","format":"wav"}}]}]}`, false},
		{"native_responses_file", "responses", "/v1/responses", `{"model":"opaque","input":[{"type":"message","role":"user","content":[{"type":"input_file","file_data":"fake-file"}]}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := map[string]string{"messages": "/v1/messages", "responses": "/responses", "chat": "/chat/completions"}[tc.wire]
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"data":[{"id":"opaque","supported_endpoints":[%q]}]}`, endpoint)
			}))
			defer server.Close()
			resolver := copilot.NewEndpointResolver(func(context.Context, string, string) (string, string, error) { return "runtime", server.URL, nil }, server.Client())
			provider := &ResponsesProvider{CopilotResolver: resolver}
			ctx, _ := httpbridge.TestContext(httptest.NewRecorder())
			httpbridge.SetRequest(ctx, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body)))
			req, _, err := provider.ConvertBodyToProviderRequest(ctx, &config.UpstreamConfig{ServiceType: "copilot"}, "github-fake", []byte(tc.body), tc.path)
			if tc.reject {
				if err == nil {
					t.Fatal("unsupported media was silently accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(req.Body)
			req.Body.Close()
			if tc.name == "tool_input_type_is_data" {
				if !strings.Contains(string(body), `image`) {
					t.Fatalf("tool arguments lost: %s", body)
				}
				return
			}
			if !strings.Contains(string(body), "picture.png") && !strings.Contains(string(body), "fake-audio") && !strings.Contains(string(body), "fake-file") {
				t.Fatalf("media disappeared: %s", body)
			}
		})
	}
}
