package types

// GeminiFinishReasonFailed distinguishes generation failures from normal
// completion, token limits, and explicit safety refusals that remain visible.
func GeminiFinishReasonFailed(reason string) bool {
	switch reason {
	case "", "STOP", "MAX_TOKENS", "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT":
		return false
	default:
		return true
	}
}
