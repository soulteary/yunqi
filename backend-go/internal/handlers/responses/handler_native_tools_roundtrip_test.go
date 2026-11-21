package responses

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/session"
)

// Exercise the actual handler against a strict Responses endpoint. Chat's nested
// function schema is deliberately rejected, as are unconverted client histories.
func TestResponsesHandler_NativeCodexToolsRoundTrip(t *testing.T) {
	for _, choiceCase := range []struct{ name, choice, upstreamName string }{
		{"namespace", `{"type":"function","namespace":"functions","name":"wait"}`, "functions__wait"},
		{"custom", `{"type":"custom","name":"exec"}`, "exec"},
		{"ordinary", `{"type":"function","name":"lookup"}`, "lookup"},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream_%t", choiceCase.name, stream), func(t *testing.T) {
				var requests atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					fail := func(format string, args ...interface{}) {
						t.Errorf(format, args...)
						w.WriteHeader(400)
					}
					if r.URL.Path != "/v1/responses" {
						fail("unexpected endpoint: %s", r.URL.Path)
						return
					}
					tools, _ := body["tools"].([]interface{})
					if len(tools) != 3 {
						fail("tools must preserve ordinary/custom/namespace declarations: %#v", tools)
						return
					}
					names := map[string]bool{}
					for _, raw := range tools {
						tool := raw.(map[string]interface{})
						if tool["type"] != "function" || tool["name"] == nil || tool["function"] != nil || tool["parameters"] == nil {
							fail("invalid native Responses function declaration: %#v", tool)
							return
						}
						names[tool["name"].(string)] = true
						if tool["name"] == "lookup" && (tool["strict"] != true || tool["description"] != "Lookup") {
							fail("ordinary function metadata lost: %#v", tool)
							return
						}
					}
					if !names["lookup"] || !names["exec"] || !names["functions__wait"] {
						fail("unexpected tool names: %#v", names)
						return
					}
					choice, _ := body["tool_choice"].(map[string]interface{})
					if choice["type"] != "function" || choice["name"] != choiceCase.upstreamName || choice["function"] != nil {
						fail("invalid native Responses tool_choice: %#v", choice)
						return
					}
					turn := requests.Add(1)
					if turn == 2 {
						input := body["input"].([]interface{})
						calls, outputs := map[string]string{}, map[string]bool{}
						for _, raw := range input {
							item := raw.(map[string]interface{})
							switch item["type"] {
							case "function_call":
								if item["namespace"] != nil || item["input"] != nil || item["arguments"] == nil {
									fail("unconverted call history: %#v", item)
									return
								}
								calls[item["call_id"].(string)] = item["name"].(string)
							case "function_call_output":
								outputs[item["call_id"].(string)] = true
							case "custom_tool_call", "custom_tool_call_output":
								fail("client-only history leaked upstream: %#v", item)
								return
							}
						}
						if calls["call_custom"] != "exec" || calls["call_ns"] != "functions__wait" || calls["call_plain"] != "lookup" || len(outputs) != 3 {
							fail("lost call/output identities: calls=%#v outputs=%#v", calls, outputs)
							return
						}
					}
					output := []interface{}{
						map[string]interface{}{"type": "function_call", "id": "fc_custom", "call_id": "call_custom", "name": "exec", "arguments": `{"input":"pwd"}`, "status": "completed"},
						map[string]interface{}{"type": "function_call", "id": "fc_ns", "call_id": "call_ns", "name": "functions__wait", "arguments": `{}`, "status": "completed"},
						map[string]interface{}{"type": "function_call", "id": "fc_plain", "call_id": "call_plain", "name": "lookup", "arguments": `{}`, "status": "completed"},
					}
					response := map[string]interface{}{"id": "resp_tools", "object": "response", "model": "gpt-5", "status": "completed", "output": output, "usage": map[string]interface{}{"input_tokens": 3, "output_tokens": 2, "total_tokens": 5}}
					if !stream {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(response)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					emit := func(event map[string]interface{}) {
						data, _ := json.Marshal(event)
						fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
					}
					for i, item := range output {
						emit(map[string]interface{}{"type": "response.output_item.added", "output_index": i, "item": item})
						emit(map[string]interface{}{"type": "response.function_call_arguments.done", "output_index": i, "item_id": item.(map[string]interface{})["id"], "arguments": item.(map[string]interface{})["arguments"]})
						emit(map[string]interface{}{"type": "response.output_item.done", "output_index": i, "item": item})
					}
					emit(map[string]interface{}{"type": "response.completed", "response": response})
				}))
				defer upstream.Close()
				up := config.UpstreamConfig{Name: "native-tools", BaseURL: upstream.URL, APIKeys: []string{"sk-test"}, ServiceType: "responses", Status: "active"}
				up.SetCompatSeed(config.TraitCodexNativeToolPassthrough, true)
				router := newResponsesTestRouter(t, up, session.NewSessionManager(time.Hour, 100, 100000))
				tools := `[{"type":"function","name":"lookup","description":"Lookup","strict":true,"parameters":{"type":"object","properties":{}}},{"type":"custom","name":"exec"},{"type":"namespace","name":"functions","tools":[{"type":"function","name":"wait","parameters":{"type":"object","properties":{}}}]}]`
				input := `[{"type":"message","role":"user","content":"test"}]`
				for turn := 1; turn <= 2; turn++ {
					body := fmt.Sprintf(`{"model":"gpt-5","store":false,"stream":%t,"tools":%s,"tool_choice":%s,"input":%s}`, stream, tools, choiceCase.choice, input)
					w := performResponsesHandlerRequest(t, router, body)
					if w.Code != 200 {
						t.Fatalf("turn %d status=%d body=%s", turn, w.Code, w.Body.String())
					}
					var response map[string]interface{}
					if stream {
						for _, line := range strings.Split(w.Body.String(), "\n") {
							if !strings.HasPrefix(line, "data: ") {
								continue
							}
							var event map[string]interface{}
							_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event)
							if event["type"] == "response.completed" {
								response, _ = event["response"].(map[string]interface{})
							}
						}
						if !strings.Contains(w.Body.String(), "response.custom_tool_call_input.done") {
							t.Fatalf("custom stream event missing: %s", w.Body.String())
						}
					} else if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					output, _ := response["output"].([]interface{})
					if len(output) != 3 {
						t.Fatalf("lost output: %s", w.Body.String())
					}
					custom, ns, plain := output[0].(map[string]interface{}), output[1].(map[string]interface{}), output[2].(map[string]interface{})
					if custom["type"] != "custom_tool_call" || custom["name"] != "exec" || custom["input"] != "pwd" || custom["call_id"] != "call_custom" || ns["name"] != "wait" || ns["namespace"] != "functions" || ns["call_id"] != "call_ns" || plain["name"] != "lookup" || plain["call_id"] != "call_plain" {
						t.Fatalf("incorrect restoration: %#v", output)
					}
					for _, id := range []string{"call_custom", "call_ns", "call_plain"} {
						typ := "function_call_output"
						if id == "call_custom" {
							typ = "custom_tool_call_output"
						}
						output = append(output, map[string]interface{}{"type": typ, "call_id": id, "output": "ok"})
					}
					encoded, _ := json.Marshal(output)
					input = string(encoded)
				}
				if requests.Load() != 2 {
					t.Fatalf("upstream request count=%d", requests.Load())
				}
			})
		}
	}
}
