package common

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/redaction"
	"github.com/soulteary/yunqi/internal/utils"
)

// BodyTextForLog is only used for diagnostic logs, never for wire bytes. Binary
// media and sensitive fields are omitted before the whole record is bounded.
func BodyTextForLog(text string) string {
	policy := config.RuntimeBodyLogging()
	if !policy.RequestBodies && !policy.ResponseBodies {
		return "[body logging disabled]"
	}
	safe := redaction.BodyJSON([]byte(text))
	if strings.HasPrefix(text, "data:") || strings.Contains(text, "\ndata:") {
		lines := strings.Split(text, "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data == "[DONE]" {
					lines[i] = line
				} else {
					lines[i] = "data: " + string(redaction.BodyJSON([]byte(data)))
				}
			} else {
				lines[i] = redaction.String(line)
			}
		}
		safe = []byte(strings.Join(lines, "\n"))
	}
	return boundBodyLogText(string(safe), policy.MaxBytes)
}
func boundBodyLogText(text string, limit int) string {
	safe := []byte(text)
	if len(safe) > limit {
		safe = safe[:limit]
		for !utf8.Valid(safe) {
			safe = safe[:len(safe)-1]
		}
		return string(safe) + fmt.Sprintf(" [truncated body log at %d bytes]", limit)
	}
	return string(safe)
}
func FormatBodyJSONForLog(data []byte, maxTextLength int) string {
	return boundBodyLogText(utils.FormatJSONBytesForLog([]byte(BodyTextForLog(string(data))), maxTextLength), config.RuntimeBodyLogging().MaxBytes)
}
func FormatBodyRawForLog(data []byte) string { return BodyTextForLog(string(data)) }

func RequestBodyTextForLog(text string) string {
	if !config.RuntimeBodyLogging().RequestBodies {
		return "[request body logging disabled]"
	}
	return BodyTextForLog(text)
}
func ResponseBodyTextForLog(text string) string {
	if !config.RuntimeBodyLogging().ResponseBodies {
		return "[response body logging disabled]"
	}
	return BodyTextForLog(text)
}
func FormatRequestBodyJSONForLog(data []byte, maxTextLength int) string {
	return boundBodyLogText(utils.FormatJSONBytesForLog([]byte(RequestBodyTextForLog(string(data))), maxTextLength), config.RuntimeBodyLogging().MaxBytes)
}
func FormatRequestBodyRawForLog(data []byte) string { return RequestBodyTextForLog(string(data)) }
func FormatResponseBodyJSONForLog(data []byte, maxTextLength int) string {
	return boundBodyLogText(utils.FormatJSONBytesForLog([]byte(ResponseBodyTextForLog(string(data))), maxTextLength), config.RuntimeBodyLogging().MaxBytes)
}
func FormatResponseBodyRawForLog(data []byte) string { return ResponseBodyTextForLog(string(data)) }

func blacklistEvidenceForLog(apiType string, status int, body []byte, result BlacklistResult) (string, string) {
	recoverAt := result.RecoverAt
	if recoverAt == "" {
		recoverAt = utils.ExtractQuotaRecoverAt(result.Message)
	}
	return errorBodySummaryForLog(apiType, status, body), recoverAt
}
