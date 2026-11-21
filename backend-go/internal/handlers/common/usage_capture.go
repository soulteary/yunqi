package common

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/soulteary/yunqi/internal/types"
)

// usageCapture observes the same bytes consumed by protocol handlers; it never
// reads ahead, blocks completion or changes the upstream response.
type usageCapture struct {
	io.ReadCloser
	mu          sync.Mutex
	stream      bool
	pending     []byte
	overflow    bool
	validJSON   bool
	finalOutput *int
	truncated   bool
	terminal    bool
	raw         map[string]json.RawMessage
}

func (r *usageCapture) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	if n > 0 {
		if r.stream {
			for _, b := range p[:n] {
				if b == '\n' {
					if !r.overflow {
						r.captureLine(r.pending)
					}
					r.pending = nil
					r.overflow = false
				} else if !r.overflow {
					if len(r.pending) >= 256<<10 {
						r.truncated = true
						r.raw = nil
						r.pending = nil
						r.overflow = true
					} else {
						r.pending = append(r.pending, b)
					}
				}
			}
		} else if !r.overflow {
			if len(r.pending)+n > 2<<20 {
				r.pending = nil
				r.overflow = true
			} else {
				r.pending = append(r.pending, p[:n]...)
			}
		}
	}
	return n, err
}

func (r *usageCapture) captureLine(line []byte) {
	line = bytes.TrimSpace(line)
	if bytes.HasPrefix(line, []byte("data:")) {
		if bytes.Equal(bytes.TrimSpace(line[5:]), []byte("[DONE]")) {
			r.terminal = true
			return
		}
		r.captureJSON(bytes.TrimSpace(line[5:]))
	}
}

func (r *usageCapture) captureJSON(body []byte) {
	if r.truncated {
		return
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil {
		return
	}
	r.validJSON = true
	var eventType string
	_ = json.Unmarshal(envelope["type"], &eventType)
	if eventType == "response.completed" || eventType == "response.incomplete" || eventType == "response.failed" || eventType == "message_stop" {
		r.terminal = true
	}
	if eventType == "message_delta" {
		var delta struct {
			StopReason string `json:"stop_reason"`
		}
		if json.Unmarshal(envelope["delta"], &delta) == nil && delta.StopReason != "" {
			r.terminal = true
		}
	}
	if eventType == "response.completed" {
		var response struct {
			Usage map[string]json.RawMessage `json:"usage"`
		}
		if json.Unmarshal(envelope["response"], &response) == nil {
			if rawOutput, ok := response.Usage["output_tokens"]; ok {
				var output int
				if json.Unmarshal(rawOutput, &output) == nil && output >= 0 {
					r.finalOutput = &output
				}
			}
		}
	}
	raw := envelope["usage"]
	if len(raw) == 0 {
		raw = envelope["usageMetadata"]
	}
	if len(raw) == 0 {
		for _, key := range []string{"message", "response"} {
			if nested := envelope[key]; len(nested) > 0 {
				r.captureJSON(nested)
			}
		}
		return
	}
	if len(raw) > 0 {
		if choices, ok := envelope["choices"]; ok {
			var items []map[string]json.RawMessage
			if json.Unmarshal(choices, &items) == nil {
				if len(items) == 0 {
					r.terminal = true
				}
				for _, item := range items {
					var reason string
					if json.Unmarshal(item["finish_reason"], &reason) == nil && reason != "" {
						r.terminal = true
					}
				}
			}
		}
		if candidates, ok := envelope["candidates"]; ok {
			var items []map[string]json.RawMessage
			if json.Unmarshal(candidates, &items) == nil {
				for _, item := range items {
					var reason string
					if json.Unmarshal(item["finishReason"], &reason) == nil && reason != "" {
						r.terminal = true
					}
				}
			}
		}
	}
	raw = types.SanitizeRawUsage(raw)
	var usage map[string]json.RawMessage
	if len(raw) == 0 || len(raw) > 64<<10 || json.Unmarshal(raw, &usage) != nil {
		r.raw = nil
		r.truncated = true
		return
	}
	if r.raw == nil {
		r.raw = make(map[string]json.RawMessage)
	}
	for key, value := range usage {
		r.raw[key] = value
	}
	encoded, _ := json.Marshal(r.raw)
	if len(r.raw) > 256 || len(encoded) > 64<<10 {
		r.raw = nil
		r.truncated = true
	}
}

func (r *usageCapture) annotate(u *types.Usage, protocol string) {
	if u == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stream {
		r.captureLine(r.pending)
	} else if !r.overflow {
		r.captureJSON(r.pending)
	}
	r.pending = nil
	switch protocol {
	case "openai":
		protocol = "chat"
	case "claude":
		protocol = "messages"
	case "copilot":
		protocol = "responses"
	}
	// A terminal Responses usage value is authoritative, including zero. It
	// supersedes earlier locally synthesized output estimates rather than being
	// lost to a positive-only merge.
	if r.finalOutput != nil {
		u.OutputTokens = *r.finalOutput
	}
	p := u.AccountingProvenance()
	if r.finalOutput != nil {
		fields := p.EstimatedFields[:0]
		for _, field := range p.EstimatedFields {
			if field != "output_tokens" {
				fields = append(fields, field)
			}
		}
		p.EstimatedFields = fields
	}
	p.Protocol = protocol
	if r.truncated {
		u.Provenance = &types.UsageProvenance{UsageSource: "unknown", Protocol: protocol, Reason: "raw_usage_limit_exceeded"}
		return
	}
	if len(r.raw) == 0 {
		if p.UsageSource == "unknown" {
			p.Reason = "upstream_usage_missing_or_unreadable"
			if r.validJSON && !r.overflow {
				p.UsageSource = "estimated"
				p.Reason = "upstream_usage_missing"
				for _, field := range []struct {
					name  string
					count int
				}{{"input_tokens", u.InputTokens}, {"output_tokens", u.OutputTokens}, {"cache_read_input_tokens", u.CacheReadInputTokens}, {"cache_creation_input_tokens", u.CacheCreationInputTokens}} {
					if field.count > 0 {
						p.EstimatedFields = append(p.EstimatedFields, field.name)
					}
				}
			}
		}
		u.Provenance = p
		return
	}
	raw, _ := json.Marshal(r.raw)
	p.RawUsage = raw
	p.UsageSource = "provider"
	p.Reason = ""
	checks := []struct {
		field string
		count int
		keys  []string
	}{
		{"input_tokens", u.InputTokens, []string{"input_tokens", "prompt_tokens", "promptTokenCount"}},
		{"output_tokens", u.OutputTokens, []string{"output_tokens", "completion_tokens", "candidatesTokenCount"}},
		{"cache_read_input_tokens", u.CacheReadInputTokens, []string{"cache_read_input_tokens", "cached_tokens", "cachedContentTokenCount", "cache_read_tokens"}},
		{"cache_creation_input_tokens", u.CacheCreationInputTokens, []string{"cache_creation_input_tokens", "cache_creation_tokens"}},
	}
	for _, check := range checks {
		if check.count <= 0 {
			continue
		}
		present := false
		for _, key := range check.keys {
			if containsUsageField(r.raw, key) {
				present = true
				break
			}
		}
		if !present {
			found := false
			for _, field := range p.EstimatedFields {
				if field == check.field {
					found = true
				}
			}
			if !found {
				p.EstimatedFields = append(p.EstimatedFields, check.field)
			}
		}
	}
	if len(p.EstimatedFields) > 0 {
		p.UsageSource = "mixed"
		p.Reason = "normalized_fields_missing_from_upstream_usage"
	}
	u.Provenance = p
}

func containsUsageField(values map[string]json.RawMessage, key string) bool {
	if value, ok := values[key]; ok {
		var count float64
		return json.Unmarshal(value, &count) == nil && count > 0
	}
	for _, value := range values {
		if strings.HasPrefix(strings.TrimSpace(string(value)), "{") {
			var nested map[string]json.RawMessage
			if json.Unmarshal(value, &nested) == nil && containsUsageField(nested, key) {
				return true
			}
		}
	}
	return false
}
