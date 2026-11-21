package types

import (
	"bytes"
	"encoding/json"
)

// UsageProvenance describes the accounting source; it is never sent on a wire
// protocol as part of Usage. Unknown means provenance was not captured.
type UsageProvenance struct {
	UsageSource     string          `json:"usage_source"`
	EstimatedFields []string        `json:"estimated_fields,omitempty"`
	Reason          string          `json:"reason,omitempty"`
	Protocol        string          `json:"protocol,omitempty"`
	RawUsage        json.RawMessage `json:"raw_usage,omitempty"`
}

func (u *Usage) AccountingProvenance() *UsageProvenance {
	if u == nil || u.Provenance == nil {
		return &UsageProvenance{UsageSource: "unknown", Reason: "source_not_captured"}
	}
	p := *u.Provenance
	p.EstimatedFields = append([]string(nil), p.EstimatedFields...)
	p.RawUsage = SanitizeRawUsage(p.RawUsage)
	if len(u.Provenance.RawUsage) > 0 && len(p.RawUsage) == 0 {
		p.UsageSource = "unknown"
		p.Reason = "raw_usage_limit_exceeded"
	}
	if p.UsageSource == "" {
		p.UsageSource = "unknown"
	}
	return &p
}

// CaptureUsageSource retains only the upstream usage object, never response text.
func CaptureUsageSource(u *Usage, protocol string, body []byte) {
	if u == nil {
		return
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil {
		return
	}
	raw := envelope["usage"]
	if protocol == "gemini" {
		raw = envelope["usageMetadata"]
	}
	if len(raw) == 0 || string(raw) == "null" || len(raw) > 64<<10 {
		return
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return
	}
	u.Provenance = &UsageProvenance{UsageSource: "provider", Protocol: protocol, RawUsage: SanitizeRawUsage(raw)}
}

// SanitizeRawUsage retains metering numbers and structure. Arbitrary upstream
// strings are never accounting data and are redacted before persistence.
func SanitizeRawUsage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return nil
	}
	var value map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || value == nil {
		return nil
	}
	fields := 0
	var sanitize func(interface{}, int) (interface{}, bool)
	sanitize = func(v interface{}, depth int) (interface{}, bool) {
		if depth > 8 {
			return nil, false
		}
		switch x := v.(type) {
		case string:
			return "[redacted]", true
		case nil, bool, json.Number:
			return x, true
		case map[string]interface{}:
			out := make(map[string]interface{}, len(x))
			for key, item := range x {
				fields++
				if fields > 256 || len(key) > 64 {
					return nil, false
				}
				for _, r := range key {
					if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
						return nil, false
					}
				}
				clean, ok := sanitize(item, depth+1)
				if !ok {
					return nil, false
				}
				out[key] = clean
			}
			return out, true
		case []interface{}:
			if len(x) > 256 {
				return nil, false
			}
			out := make([]interface{}, len(x))
			for i, item := range x {
				clean, ok := sanitize(item, depth+1)
				if !ok {
					return nil, false
				}
				out[i] = clean
			}
			return out, true
		default:
			return nil, false
		}
	}
	clean, ok := sanitize(value, 0)
	if !ok {
		return nil
	}
	encoded, err := json.Marshal(clean)
	if err != nil || len(encoded) > 64<<10 {
		return nil
	}
	return encoded
}
