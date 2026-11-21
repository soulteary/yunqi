package responses

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWebSocketContinuationPreservesToolAndReasoningHistory(t *testing.T) {
	var cache responsesWebSocketContinuation
	first := []byte(`{"model":"test","input":"start","store":false}`)
	_, input, replayable := cache.prepare(first)
	var turn responsesWebSocketTurn
	turn.observe(`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","encrypted_content":"encrypted-state","future_field":"keep"}}`)
	turn.observe(`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","call_id":"call_1","name":"lookup","namespace":"tools","arguments":"{}"}}`)
	turn.observe(`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[]}}`)
	cache.record(input, replayable, &turn)
	request := []byte(`{"previous_response_id":"resp_1","input":[{"type":"function_call_output","call_id":"call_1","output":"result"}],"store":false,"tools":[{"type":"function","name":"lookup"}]}`)
	expanded, items, ok := cache.prepare(request)
	if !ok || len(items) != 4 {
		t.Fatalf("tool turn was not expanded exactly once: %s", expanded)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(expanded, &got); err != nil {
		t.Fatal(err)
	}
	if got["previous_response_id"] != nil || string(got["store"]) != "false" || got["tools"] == nil {
		t.Fatalf("request fields changed unexpectedly: %s", expanded)
	}
	for _, want := range []string{"encrypted-state", "future_field", "keep", "call_1", "namespace", "result"} {
		if !strings.Contains(string(expanded), want) {
			t.Errorf("lost %q in replay: %s", want, expanded)
		}
	}
	var second responsesWebSocketTurn
	second.observe(`{"type":"response.completed","response":{"id":"resp_2","status":"completed","output":[{"type":"message","role":"assistant","content":"done"}]}}`)
	cache.record(items, true, &second)
	_, next, ok := cache.prepare([]byte(`{"previous_response_id":"resp_2","input":"again"}`))
	if !ok || len(next) != 6 {
		t.Fatalf("expanded history was duplicated or dropped: len=%d replayable=%t", len(next), ok)
	}
}

func TestWebSocketContinuationKeepsUnresolvedCursors(t *testing.T) {
	var cache responsesWebSocketContinuation
	_, input, replayable := cache.prepare([]byte(`{"input":"hello"}`))
	var done responsesWebSocketTurn
	done.observe(`{"type":"response.completed","response":{"id":"known","output":[]}}`)
	cache.record(input, replayable, &done)
	for _, typ := range []string{"response.incomplete", "response.failed"} {
		var turn responsesWebSocketTurn
		turn.observe(`{"type":"` + typ + `","response":{"id":"failed","output":[]}}`)
		cache.record(input, true, &turn)
		request := []byte(`{"previous_response_id":"failed","input":"continue"}`)
		actual, _, ok := cache.prepare(request)
		if ok || string(actual) != string(request) {
			t.Fatalf("%s cursor was silently discarded: %s", typ, actual)
		}
	}
	var otherConnection responsesWebSocketContinuation
	request := []byte(`{"previous_response_id":"known","input":"other connection"}`)
	actual, _, ok := otherConnection.prepare(request)
	if ok || string(actual) != string(request) {
		t.Fatal("connection-local history leaked to another connection")
	}
	// An older cursor remains an upstream concern after the latest snapshot advances.
	cache.record(input, true, &done)
	done.id = "newer"
	cache.record(input, true, &done)
	actual, _, ok = cache.prepare(request)
	if ok || string(actual) != string(request) {
		t.Fatal("an older cursor was replayed using newer history")
	}
}

func TestWebSocketContinuationLimitFallsBackWithoutTruncatingInput(t *testing.T) {
	large, _ := json.Marshal(map[string]interface{}{"type": "message", "role": "user", "content": strings.Repeat("x", responsesWebSocketReadLimit)})
	var cache responsesWebSocketContinuation
	cache.record([]json.RawMessage{large}, true, &responsesWebSocketTurn{id: "large", completed: true})
	if cache.id != "" || cache.history != nil {
		t.Fatal("oversized history was retained")
	}
	request := []byte(`{"previous_response_id":"large","input":"next"}`)
	actual, _, ok := cache.prepare(request)
	if ok || string(actual) != string(request) {
		t.Fatal("oversized history silently removed the upstream cursor")
	}
	cache.id, cache.history = "large", []json.RawMessage{large}
	actual, _, ok = cache.prepare(request)
	if ok || string(actual) != string(request) {
		t.Fatal("expanded input exceeded its bound instead of preserving the cursor")
	}
}

func TestWebSocketContinuationKeepsCursorWhenOutputIsOnlyPartial(t *testing.T) {
	var cache responsesWebSocketContinuation
	var turn responsesWebSocketTurn
	turn.observe(`{"type":"response.output_text.delta","output_index":0,"delta":"partial answer"}`)
	turn.observe(`{"type":"response.completed","response":{"id":"partial","output":[]}}`)
	cache.record(nil, true, &turn)
	request := []byte(`{"previous_response_id":"partial","input":"next"}`)
	actual, _, ok := cache.prepare(request)
	if ok || string(actual) != string(request) {
		t.Fatal("partial output caused a context-losing replay")
	}
}

func TestWebSocketContinuationDoesNotCacheUnindexedDeltasOrIncompleteStatus(t *testing.T) {
	for _, events := range [][]string{
		{`{"type":"response.output_text.delta","delta":"partial"}`, `{"type":"response.completed","response":{"id":"unsafe","output":[]}}`},
		{`{"type":"response.completed","response":{"id":"unsafe","status":"incomplete","output":[{"type":"message","content":"partial"}]}}`},
	} {
		var cache responsesWebSocketContinuation
		var turn responsesWebSocketTurn
		for _, event := range events {
			turn.observe(event)
		}
		cache.record(nil, true, &turn)
		request := []byte(`{"previous_response_id":"unsafe","input":"next"}`)
		actual, _, ok := cache.prepare(request)
		if ok || string(actual) != string(request) {
			t.Fatal("unsafe output was replayed as complete history")
		}
	}
}

func TestWebSocketContinuationBoundsItemCount(t *testing.T) {
	input := "[" + strings.TrimSuffix(strings.Repeat(`{},`, maxResponsesWebSocketHistoryItems+1), ",") + "]"
	cache := responsesWebSocketContinuation{}
	body := []byte(`{"input":` + input + `}`)
	actual, _, ok := cache.prepare(body)
	if ok || string(actual) != string(body) {
		t.Fatal("oversized item count was retained or truncated")
	}
}

func TestWebSocketContinuationOrdersCompletedItemsByOutputIndex(t *testing.T) {
	var cache responsesWebSocketContinuation
	var turn responsesWebSocketTurn
	turn.observe(`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","call_id":"call_1","name":"test","arguments":"{}"}}`)
	turn.observe(`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","encrypted_content":"state"}}`)
	turn.observe(`{"type":"response.completed","response":{"id":"ordered","output":[]}}`)
	cache.record(nil, true, &turn)
	_, items, ok := cache.prepare([]byte(`{"previous_response_id":"ordered","input":[]}`))
	if !ok || len(items) != 2 || !strings.Contains(string(items[0]), `"reasoning"`) || !strings.Contains(string(items[1]), `"call_1"`) {
		t.Fatalf("parallel tool completion reordered output history: %s", items)
	}
}
