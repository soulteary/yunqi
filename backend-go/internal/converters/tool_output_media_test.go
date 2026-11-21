package converters

import (
	"encoding/json"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/tidwall/gjson"
	"strings"
	"testing"
)

func TestMultimodalParallelToolResultsChatWire(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"type":"function_call","call_id":"a","name":"image","arguments":"{}"},{"type":"function_call","call_id":"b","name":"file","arguments":"{}"},{"type":"function_call_output","call_id":"a","output":[{"type":"input_text","text":"image ready"},{"type":"input_image","image_url":"data:image/png;base64,aW1hZ2U="}]},{"type":"function_call_output","call_id":"b","output":[{"type":"input_file","file_data":"data:application/pdf;base64,cGRm","filename":"result.pdf"}]}]}`)
	var req types.ResponsesRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	typed, err := (&OpenAIChatConverter{}).ToProviderRequest(&session.Session{}, &req)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(typed)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{ConvertResponsesToOpenAIChatRequest("m", raw, false), encoded} {
		messages := gjson.GetBytes(body, "messages").Array()
		if len(messages) != 4 || messages[1].Get("role").String() != "tool" || messages[2].Get("role").String() != "tool" || messages[3].Get("role").String() != "user" {
			t.Fatalf("parallel pairing broken %s", body)
		}
		if messages[1].Get("tool_call_id").String() != "a" || messages[2].Get("tool_call_id").String() != "b" {
			t.Fatalf("call ID lost %s", body)
		}
		if strings.Contains(messages[1].Get("content").String(), "base64") || strings.Contains(messages[2].Get("content").String(), "base64") {
			t.Fatalf("base64 degraded into tool text: %s", body)
		}
		if messages[3].Get("content.1.image_url.url").String() != "data:image/png;base64,aW1hZ2U=" || messages[3].Get("content.3.file.filename").String() != "result.pdf" {
			t.Fatalf("media lost %s", body)
		}
	}
}

func TestStructuredChatToolOutputRoundTrips(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"tool","tool_call_id":"a","content":[{"type":"text","text":"result"}]}]}`)
	converted := ConvertChatRequestToResponsesRequest(raw)
	if !gjson.GetBytes(converted, "input.0.output").IsArray() {
		t.Fatalf("array stringified %s", converted)
	}
}

func TestClaudeToolMediaRemainsStructured(t *testing.T) {
	var req types.ResponsesRequest
	raw := []byte(`{"input":[{"type":"function_call_output","call_id":"a","output":[{"type":"input_text","text":"result"},{"type":"input_image","image_url":"data:image/png;base64,aW1hZ2U="},{"type":"input_file","filename":"r.pdf","file_data":"data:application/pdf;base64,cGRm"}]}]}`)
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	converted, err := (&ClaudeConverter{}).ToProviderRequest(&session.Session{}, &req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(converted)
	if err != nil {
		t.Fatal(err)
	}
	output := gjson.GetBytes(body, "messages.0.content.0.content")
	if output.Get("1.type").String() != "image" || output.Get("1.source.data").String() != "aW1hZ2U=" || output.Get("2.type").String() != "document" || output.Get("2.source.data").String() != "cGRm" {
		t.Fatalf("media lost %s", body)
	}
}

func TestGeminiToolMediaNativePartsRoundTrip(t *testing.T) {
	var req types.ResponsesRequest
	raw := []byte(`{"input":[{"type":"function_call_output","call_id":"a","output":[{"type":"input_text","text":"result"},{"type":"input_image","image_url":"data:image/png;base64,aW1hZ2U="},{"type":"input_file","filename":"r.pdf","file_data":"data:application/pdf;base64,cGRm"}]}]}`)
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	converted, err := ResponsesToGeminiRequest(&session.Session{}, &req, "m")
	if err != nil {
		t.Fatal(err)
	}
	response := converted.Contents[0].Parts[0].FunctionResponse
	if len(response.Parts) != 2 || response.Parts[0].InlineData.Data != "aW1hZ2U=" || response.Parts[1].InlineData.Data != "cGRm" {
		t.Fatalf("native parts %#v", response)
	}
	payload, _ := json.Marshal(response.Response)
	if strings.Contains(string(payload), "aW1hZ2U=") || strings.Contains(string(payload), "cGRm") {
		t.Fatalf("base64 in ordinary response JSON: %s", payload)
	}
	output := geminiFunctionResponseOutput(response)
	encoded, _ := json.Marshal(output)
	if !strings.Contains(string(encoded), "data:image/png;base64,aW1hZ2U=") || !strings.Contains(string(encoded), "data:application/pdf;base64,cGRm") {
		t.Fatalf("roundtrip %s", encoded)
	}
}

func TestClaudeRejectsNonportableToolFileID(t *testing.T) {
	if _, err := ConvertToolOutputToClaude([]interface{}{map[string]interface{}{"type": "input_file", "file_id": "file-provider-specific"}}); err == nil {
		t.Fatal("nonportable file ID silently accepted")
	}
}

func TestGeminiMixedToolMediaPortableOrRejected(t *testing.T) {
	for _, tc := range []struct {
		name, part string
		reject     bool
	}{
		{"url", `{"type":"input_file","file_url":"https://example.test/report.pdf"}`, false},
		{"id", `{"type":"input_file","file_id":"foreign-id"}`, true},
		{"unknown-mime", `{"type":"input_file","file_url":"https://example.test/opaque"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req types.ResponsesRequest
			raw := `{"input":[{"type":"function_call_output","call_id":"a","output":[{"type":"input_image","image_url":"data:image/png;base64,aW1hZ2U="},` + tc.part + `]}]}`
			if err := json.Unmarshal([]byte(raw), &req); err != nil {
				t.Fatal(err)
			}
			got, err := ResponsesToGeminiRequest(&session.Session{}, &req, "m")
			if tc.reject {
				if err == nil {
					t.Fatal("nonportable media accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r := got.Contents[0].Parts[0].FunctionResponse
			if len(r.Parts) != 2 || r.Parts[0].InlineData == nil || r.Parts[1].FileData == nil || r.Parts[1].FileData.MimeType != "application/pdf" {
				t.Fatalf("media lost: %#v", r)
			}
			body, _ := json.Marshal(r.Response)
			if strings.Contains(string(body), "aW1hZ2U=") {
				t.Fatalf("base64 text fallback %s", body)
			}
		})
	}
}

func TestGeminiNativeParallelResultsPreserveIDs(t *testing.T) {
	var req types.GeminiRequest
	raw := `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"a","name":"same","args":{}}},{"functionCall":{"id":"b","name":"same","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"id":"a","name":"same","response":{"output":"first"},"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}},{"functionResponse":{"id":"b","name":"same","response":{"output":"second"}}}]}]}`
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	chat, err := GeminiToOpenAIRequest(&req, "m")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(chat)
	root := gjson.ParseBytes(body)
	if root.Get("messages.0.tool_calls.0.id").String() != "a" || root.Get("messages.0.tool_calls.1.id").String() != "b" || root.Get("messages.1.tool_call_id").String() != "a" || root.Get("messages.2.tool_call_id").String() != "b" || root.Get("messages.3.role").String() != "user" {
		t.Fatalf("chat pairing lost %s", body)
	}
	claude, err := GeminiToClaudeRequest(&req, "m")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(claude)
	root = gjson.ParseBytes(body)
	if root.Get("messages.1.content.0.tool_use_id").String() != "a" || root.Get("messages.1.content.1.tool_use_id").String() != "b" || root.Get("messages.1.content.0.content.1.source.data").String() != "aW1hZ2U=" {
		t.Fatalf("Claude pairing/media lost %s", body)
	}
	responses, err := GeminiToResponsesRequest(&req, "m")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(responses)
	root = gjson.ParseBytes(body)
	if root.Get("input.2.call_id").String() != "a" || root.Get("input.3.call_id").String() != "b" {
		t.Fatalf("Responses pairing lost %s", body)
	}
}

func TestGeminiNativeMissingIDsPairByNameFIFO(t *testing.T) {
	for _, count := range []int{1, 2} {
		req := types.GeminiRequest{Contents: []types.GeminiContent{{Role: "model"}, {Role: "user"}}}
		for i := 0; i < count; i++ {
			req.Contents[0].Parts = append(req.Contents[0].Parts, types.GeminiPart{FunctionCall: &types.GeminiFunctionCall{Name: "same", Args: map[string]interface{}{}}})
			req.Contents[1].Parts = append(req.Contents[1].Parts, types.GeminiPart{FunctionResponse: &types.GeminiFunctionResponse{Name: "same", Response: map[string]interface{}{"output": i}}})
		}
		chat, err := GeminiToOpenAIRequest(&req, "m")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(chat)
		r := gjson.ParseBytes(body)
		calls := r.Get("messages.0.tool_calls").Array()
		messages := r.Get("messages").Array()
		seen := map[string]bool{}
		for i := 0; i < count; i++ {
			id := calls[i].Get("id").String()
			if id == "" || seen[id] || messages[i+1].Get("tool_call_id").String() != id {
				t.Fatalf("missing/FIFO IDs broken %s", body)
			}
			seen[id] = true
		}
		claude, err := GeminiToClaudeRequest(&req, "m")
		if err != nil {
			t.Fatal(err)
		}
		body, _ = json.Marshal(claude)
		r = gjson.ParseBytes(body)
		calls = r.Get("messages.0.content").Array()
		results := r.Get("messages.1.content").Array()
		for i := 0; i < count; i++ {
			if calls[i].Get("id").String() != results[i].Get("tool_use_id").String() {
				t.Fatalf("Claude unmatched %s", body)
			}
		}
		responses, err := GeminiToResponsesRequest(&req, "m")
		if err != nil {
			t.Fatal(err)
		}
		body, _ = json.Marshal(responses)
		items := gjson.GetBytes(body, "input").Array()
		for i := 0; i < count; i++ {
			if items[i].Get("call_id").String() != items[count+i].Get("call_id").String() {
				t.Fatalf("Responses unmatched %s", body)
			}
		}
		if req.Contents[0].Parts[0].FunctionCall.ID != "" || req.Contents[1].Parts[0].FunctionResponse.ID != "" {
			t.Fatal("caller history mutated")
		}
	}
}

func TestGeminiNativeMissingIDWithoutMatchingCallIsRejected(t *testing.T) {
	req := &types.GeminiRequest{Contents: []types.GeminiContent{{Role: "user", Parts: []types.GeminiPart{{FunctionResponse: &types.GeminiFunctionResponse{Name: "orphan", Response: map[string]interface{}{"output": "value"}}}}}}}
	for _, convert := range []func(*types.GeminiRequest, string) (map[string]interface{}, error){GeminiToOpenAIRequest, GeminiToClaudeRequest, GeminiToResponsesRequest} {
		if _, err := convert(req, "m"); err == nil {
			t.Fatal("unpaired result silently accepted")
		}
	}
}
