package types

import "encoding/json"

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
	p.RawUsage = append(json.RawMessage(nil), p.RawUsage...)
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
	u.Provenance = &UsageProvenance{UsageSource: "provider", Protocol: protocol, RawUsage: append(json.RawMessage(nil), raw...)}
}
