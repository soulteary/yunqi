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
