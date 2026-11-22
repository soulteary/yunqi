package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// GeminiFinishReasonFailed distinguishes generation failures from normal
// completion, token limits, and explicit safety refusals that remain visible.
func GeminiFinishReasonFailed(reason string) bool {
	switch reason {
	case "", "FINISH_REASON_UNSPECIFIED", "STOP", "MAX_TOKENS", "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT":
		return false
	default:
		return true
	}
}

// ParseGeminiFinishReason accepts null/absence as an omitted terminal reason,
// while rejecting malformed typed values instead of coercing them to success.
func ParseGeminiFinishReason(value interface{}) (string, error) {
	if value == nil {
		return "", nil
	}
	reason, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("Gemini finishReason must be a string or null")
	}
	return reason, nil
}

func ValidateGeminiFinishReasons(data []byte) error {
	var response struct {
		Candidates []map[string]json.RawMessage `json:"candidates"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil
	}
	for _, candidate := range response.Candidates {
		raw, present := candidate["finishReason"]
		if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		var reason string
		if err := json.Unmarshal(raw, &reason); err != nil {
			return fmt.Errorf("Gemini finishReason must be a string or null")
		}
	}
	return nil
}

// GeminiFinishReasonTerminal excludes the standard placeholder/default enum.
func GeminiFinishReasonTerminal(reason string) bool {
	return reason != "" && reason != "FINISH_REASON_UNSPECIFIED"
}

// GeminiPromptBlocked excludes the unused enum default from actual refusals.
func GeminiPromptBlocked(reason string) bool {
	reason = strings.TrimSpace(reason)
	return reason != "" && reason != "BLOCK_REASON_UNSPECIFIED"
}
