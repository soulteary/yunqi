package common

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/telemetry"
	"net/http"
)

func diagnosticKeyUID(key string) string {
	h := sha256.Sum256([]byte(key))
	return "key_" + hex.EncodeToString(h[:16])
}
func recordLocalDiagnostic(c fiber.Ctx, channel, keyUID, key, protocol, stage, source string) {
	d := telemetry.DiagnosticsFromContext(httpbridge.Request(c).Context())
	if d == nil {
		return
	}
	if keyUID == "" {
		keyUID = diagnosticKeyUID(key)
	}
	id := GenerateRequestID()
	d.Attempt(telemetry.DiagnosticAttempt{AttemptID: id, ChannelUID: channel, KeyUID: keyUID, Protocol: protocol, Source: "proxy", Status: "failed", ErrorSource: source})
	d.Event(stage, "rejected", id)
}
func diagnosticUpstreamRequestID(headers http.Header) string {
	for _, name := range []string{"x-request-id", "request-id", "x-goog-request-id", "x-amzn-requestid"} {
		if value := headers.Get(name); value != "" {
			return value
		}
	}
	return ""
}
func diagnosticStreamErrorSource(err error) string {
	var rejected *UpstreamRequestRejectedError
	if errors.As(err, &rejected) || errors.Is(err, ErrEmptyStreamResponse) || errors.Is(err, ErrInvalidResponseBody) || errors.Is(err, ErrEmptyNonStreamResponse) {
		return "upstream"
	}
	if errors.Is(err, ErrStreamFirstContentTimeout) || errors.Is(err, ErrStreamStalled) || errors.Is(err, ErrStreamPostCommitStalled) || errors.Is(err, ErrStreamRepetitionLoop) {
		return "gateway"
	}
	if IsClientDisconnectError(err) {
		return "client"
	}
	return "" // Arbitrary converter/I/O errors have no reliable attribution.
}
