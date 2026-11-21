package responses

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

const maxResponsesWebSocketHistoryItems = 4096

func webSocketRawItems(raw json.RawMessage) ([]json.RawMessage, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return nil, false
	}
	var items []json.RawMessage
	for decoder.More() {
		if len(items) >= maxResponsesWebSocketHistoryItems {
			return nil, false
		}
		var item json.RawMessage
		if decoder.Decode(&item) != nil {
			return nil, false
		}
		items = append(items, item)
	}
	_, err = decoder.Token()
	return items, err == nil
}

// Only the latest completed cursor on this connection is replayable. Unknown,
// failed or oversized histories retain their upstream cursor instead of losing context.
type responsesWebSocketContinuation struct {
	id      string
	history []json.RawMessage
}

type responsesWebSocketTurn struct {
	id              string
	output          []json.RawMessage
	bytes           int
	completed       bool
	unsafe          bool
	indexed         map[int]json.RawMessage
	pending         map[int]bool
	needsFullOutput bool
}

func webSocketInputItems(raw json.RawMessage) ([]json.RawMessage, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, true
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		item, _ := json.Marshal(map[string]interface{}{"type": "message", "role": "user", "content": text})
		return []json.RawMessage{item}, true
	}
	return webSocketRawItems(raw)
}

func webSocketHistorySize(items []json.RawMessage) int {
	size := 0
	for _, item := range items {
		size += len(item)
	}
	return size
}

func (c *responsesWebSocketContinuation) prepare(body []byte) ([]byte, []json.RawMessage, bool) {
	var request map[string]json.RawMessage
	if json.Unmarshal(body, &request) != nil {
		return body, nil, false
	}
	items, ok := webSocketInputItems(request["input"])
	if !ok {
		return body, nil, false
	}
	var previous string
	if raw := request["previous_response_id"]; len(raw) > 0 && json.Unmarshal(raw, &previous) != nil {
		return body, nil, false
	}
	if previous != "" {
		if previous != c.id || c.history == nil {
			return body, nil, false
		}
		items = append(append([]json.RawMessage(nil), c.history...), items...)
		if len(items) > maxResponsesWebSocketHistoryItems || webSocketHistorySize(items) > responsesWebSocketReadLimit {
			return body, nil, false
		}
		request["input"], _ = json.Marshal(items)
		delete(request, "previous_response_id")
		expanded, err := json.Marshal(request)
		if err != nil || len(expanded) > responsesWebSocketReadLimit {
			return body, nil, false
		}
		body = expanded
	}
	return body, items, true
}

func (c *responsesWebSocketContinuation) record(input []json.RawMessage, replayable bool, turn *responsesWebSocketTurn) {
	if turn == nil {
		return
	}
	if turn.unsafe {
		c.id, c.history = "", nil
		return
	}
	if !turn.completed {
		return
	}
	c.id, c.history = "", nil
	if !replayable || turn.unsafe || turn.id == "" || len(input)+len(turn.output) > maxResponsesWebSocketHistoryItems || webSocketHistorySize(input)+webSocketHistorySize(turn.output) > responsesWebSocketReadLimit {
		return
	}
	c.id = turn.id
	c.history = append(append(make([]json.RawMessage, 0, len(input)+len(turn.output)), input...), turn.output...)
}

func (t *responsesWebSocketTurn) observe(data string) {
	if t.unsafe {
		return
	}
	if len(data) > responsesWebSocketReadLimit {
		t.unsafe = true
		t.output = nil
		return
	}
	var event map[string]json.RawMessage
	if json.Unmarshal([]byte(data), &event) != nil {
		return
	}
	var typ string
	_ = json.Unmarshal(event["type"], &typ)
	var index int
	hasIndex := len(event["output_index"]) > 0 && json.Unmarshal(event["output_index"], &index) == nil && index >= 0
	if !hasIndex && (typ == "response.output_item.added" || strings.HasSuffix(typ, ".delta")) {
		t.needsFullOutput = true
	}
	if hasIndex && typ != "response.output_item.done" {
		if t.pending == nil {
			t.pending = make(map[int]bool)
		}
		t.pending[index] = true
		if len(t.pending) > maxResponsesWebSocketHistoryItems {
			t.unsafe = true
			t.pending, t.indexed, t.output = nil, nil, nil
			return
		}
	}
	switch typ {
	case "response.failed", "response.incomplete":
		t.unsafe = true
		t.output = nil
	case "response.output_item.done":
		if item := event["item"]; len(item) > 0 && string(item) != "null" {
			t.bytes += len(item)
			if t.bytes > responsesWebSocketReadLimit || len(t.indexed)+len(t.output) >= maxResponsesWebSocketHistoryItems {
				t.unsafe = true
				t.output = nil
				t.indexed = nil
				return
			}
			if hasIndex {
				if t.indexed == nil {
					t.indexed = make(map[int]json.RawMessage)
				}
				t.indexed[index] = item
				delete(t.pending, index)
			} else {
				t.output = append(t.output, item)
			}
		}
	case "response.completed":
		var response map[string]json.RawMessage
		if json.Unmarshal(event["response"], &response) != nil {
			return
		}
		_ = json.Unmarshal(response["id"], &t.id)
		var status string
		_ = json.Unmarshal(response["status"], &status)
		if status != "" && status != "completed" {
			t.unsafe = true
			return
		}
		output, ok := webSocketRawItems(response["output"])
		if !ok {
			t.unsafe = true
			return
		}
		if len(output) > 0 {
			t.output = output
		} else if len(t.pending) > 0 || t.needsFullOutput {
			// Deltas without a complete item are insufficient to replay the conversation.
			t.unsafe = true
			return
		} else if len(t.indexed) > 0 {
			indexes := make([]int, 0, len(t.indexed))
			for index := range t.indexed {
				indexes = append(indexes, index)
			}
			sort.Ints(indexes)
			for _, index := range indexes {
				t.output = append(t.output, t.indexed[index])
			}
		}
		t.completed = true
	}
}
