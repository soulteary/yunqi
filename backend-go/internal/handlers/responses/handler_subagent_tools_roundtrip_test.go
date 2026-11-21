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

// 子代理属于客户端工具。使用普通 Chat mock 验证协议，不依赖供应商、
// 模型名称或真实代理执行；同名并行 spawn 调用必须保持独立的 call_id。
func TestResponsesHandler_SubagentToolsRoundTrip(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("compat_%t/stream_%t", enabled, stream), func(t *testing.T) {
				var requests atomic.Int32
				names := []string{"spawn_agent", "spawn_agent", "wait", "send_message"}
				arguments := []string{`{"message":"first"}`, `{"message":"second"}`, `{"ids":["agent_a","agent_b"]}`, `{"id":"agent_a","message":"continue"}`}
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					fail := func(format string, args ...interface{}) { t.Errorf(format, args...); w.WriteHeader(400) }
					if r.URL.Path != "/v1/chat/completions" {
						fail("unexpected endpoint: %s", r.URL.Path)
						return
					}
					tools, _ := body["tools"].([]interface{})
					wantTools := 1
					if enabled {
						wantTools = 4
					}
					if len(tools) != wantTools {
						fail("tools=%#v, want %d declarations", tools, wantTools)
						return
					}
					declared := map[string]bool{}
					for _, raw := range tools {
						tool := raw.(map[string]interface{})
						fn, _ := tool["function"].(map[string]interface{})
						name, _ := fn["name"].(string)
						if tool["type"] != "function" || name == "" || fn["parameters"] == nil {
							fail("invalid Chat tool: %#v", tool)
							return
						}
						declared[name] = true
					}
					if !declared["lookup"] {
						fail("ordinary function lost: %#v", declared)
						return
					}
					if enabled {
						for _, name := range names {
							if !declared["collaboration__"+name] {
								fail("namespace tool lost: %s", name)
								return
							}
						}
						if body["parallel_tool_calls"] != true {
							fail("parallel_tool_calls lost")
							return
						}
					}
					turn := requests.Add(1)
					if enabled && turn == 2 {
						messages, _ := body["messages"].([]interface{})
						pending := map[string]bool{}
						calls, outputs := 0, 0
						for _, raw := range messages {
							message := raw.(map[string]interface{})
							if message["role"] == "tool" {
								id, _ := message["tool_call_id"].(string)
								if !pending[id] {
									fail("orphan or duplicate tool result: %#v", message)
									return
								}
								if message["content"] != "result_"+id {
									fail("result attached to wrong call_id: %#v", message)
									return
								}
								delete(pending, id)
								outputs++
								continue
							}
							if len(pending) != 0 {
								fail("tool results must immediately follow assistant calls: %#v", message)
								return
							}
							toolCalls, _ := message["tool_calls"].([]interface{})
							for _, rawCall := range toolCalls {
								call := rawCall.(map[string]interface{})
								id := call["id"].(string)
								fn := call["function"].(map[string]interface{})
								index := -1
								for i := range names {
									if id == fmt.Sprintf("call_%d", i) {
										index = i
									}
								}
								if index < 0 || pending[id] || fn["name"] != "collaboration__"+names[index] || fn["arguments"] != arguments[index] {
									fail("call identity/name/arguments changed: %#v", call)
									return
								}
								pending[id] = true
								calls++
							}
						}
						if calls != 4 || outputs != 4 || len(pending) != 0 {
							fail("incomplete parallel roundtrip calls=%d outputs=%d pending=%#v", calls, outputs, pending)
							return
						}
					}
					toolCalls := []interface{}{}
					if enabled {
						for i, name := range names {
							toolCalls = append(toolCalls, map[string]interface{}{"id": fmt.Sprintf("call_%d", i), "type": "function", "function": map[string]interface{}{"name": "collaboration__" + name, "arguments": arguments[i]}})
						}
					}
					usage := map[string]interface{}{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}
					finish := "stop"
					message := map[string]interface{}{"role": "assistant", "content": "ok"}
					if enabled {
						finish = "tool_calls"
						message["content"] = nil
						message["tool_calls"] = toolCalls
					}
					if !stream {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "chat_subagent", "model": "test-model", "choices": []interface{}{map[string]interface{}{"index": 0, "message": message, "finish_reason": finish}}, "usage": usage})
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					emit := func(delta map[string]interface{}, stop interface{}, withUsage bool) {
						event := map[string]interface{}{"id": "chat_subagent", "model": "test-model", "choices": []interface{}{map[string]interface{}{"index": 0, "delta": delta, "finish_reason": stop}}}
						if withUsage {
							event["usage"] = usage
						}
						data, _ := json.Marshal(event)
						fmt.Fprintf(w, "data: %s\n\n", data)
					}
					if enabled {
						// 先交错发送每个工具的声明，再以反序分片发送参数，防止按
						// 最近工具或函数名合并同名 spawn 调用。
						for i, name := range names {
							emit(map[string]interface{}{"tool_calls": []interface{}{map[string]interface{}{"index": i, "id": fmt.Sprintf("call_%d", i), "type": "function", "function": map[string]interface{}{"name": "collaboration__" + name, "arguments": ""}}}}, nil, false)
						}
						for i := len(names) - 1; i >= 0; i-- {
							cut := len(arguments[i]) / 2
							for _, part := range []string{arguments[i][:cut], arguments[i][cut:]} {
								emit(map[string]interface{}{"tool_calls": []interface{}{map[string]interface{}{"index": i, "function": map[string]interface{}{"arguments": part}}}}, nil, false)
							}
						}
					} else {
						emit(map[string]interface{}{"content": "ok"}, nil, false)
					}
					emit(map[string]interface{}{}, finish, true)
					fmt.Fprint(w, "data: [DONE]\n\n")
				}))
				defer upstream.Close()
				up := config.UpstreamConfig{Name: "subagent", BaseURL: upstream.URL, APIKeys: []string{"sk-test"}, ServiceType: "openai", Status: "active"}
				up.CodexToolCompat = &enabled
				router := newResponsesTestRouter(t, up, session.NewSessionManager(time.Hour, 100, 100000))
				tools := `[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}}},{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"spawn_agent","parameters":{"type":"object","properties":{"message":{"type":"string"}}}},{"type":"function","name":"wait","parameters":{"type":"object","properties":{"ids":{"type":"array","items":{"type":"string"}}}}},{"type":"function","name":"send_message","parameters":{"type":"object","properties":{"id":{"type":"string"},"message":{"type":"string"}}}}]}]`
				input := `[{"type":"message","role":"user","content":"delegate tasks"}]`
				for turn := 1; turn <= 2; turn++ {
					body := fmt.Sprintf(`{"model":"test-model","stream":%t,"store":false,"parallel_tool_calls":true,"tools":%s,"tool_choice":"auto","input":%s}`, stream, tools, input)
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
					} else if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
						t.Fatal(err)
					}
					output, _ := response["output"].([]interface{})
					if !enabled {
						if len(output) != 1 {
							t.Fatalf("control output=%#v", response)
						}
						continue
					}
					seen := map[string]bool{}
					for _, raw := range output {
						item := raw.(map[string]interface{})
						id, _ := item["call_id"].(string)
						index := -1
						for i := range names {
							if id == fmt.Sprintf("call_%d", i) {
								index = i
							}
						}
						if index < 0 || seen[id] || item["type"] != "function_call" || item["name"] != names[index] || item["namespace"] != "collaboration" || item["arguments"] != arguments[index] {
							t.Fatalf("tool response identity changed: %#v", item)
						}
						seen[id] = true
					}
					if len(seen) != 4 {
						t.Fatalf("lost parallel calls: %#v", output)
					}
					// 故意以反序返回结果，验证按 call_id 配对而非索引。
					for i := len(names) - 1; i >= 0; i-- {
						output = append(output, map[string]interface{}{"type": "function_call_output", "call_id": fmt.Sprintf("call_%d", i), "output": fmt.Sprintf("result_call_%d", i)})
					}
					encoded, _ := json.Marshal(output)
					input = string(encoded)
				}
				if requests.Load() != 2 {
					t.Fatalf("upstream requests=%d", requests.Load())
				}
			})
		}
	}
}
