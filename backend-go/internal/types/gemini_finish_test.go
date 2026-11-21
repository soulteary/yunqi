package types

import "testing"

func TestGeminiFinishReasonTypeContract(t *testing.T) {
	for _, value := range []string{`42`, `false`, `{}`} {
		if err := ValidateGeminiFinishReasons([]byte(`{"candidates":[{"finishReason":` + value + `}]}`)); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	for _, raw := range []string{`{"candidates":[{}]}`, `{"candidates":[{"finishReason":null}]}`, `{"candidates":[{"finishReason":"STOP"}]}`} {
		if err := ValidateGeminiFinishReasons([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGeminiUnspecifiedFinishReasonIsNotFailureOrTerminal(t *testing.T) {
	if GeminiFinishReasonFailed("FINISH_REASON_UNSPECIFIED") || GeminiFinishReasonTerminal("FINISH_REASON_UNSPECIFIED") {
		t.Fatal("standard placeholder treated as a generation terminal")
	}
	if !GeminiFinishReasonTerminal("STOP") || !GeminiFinishReasonFailed("UNKNOWN_NEW_ERROR") {
		t.Fatal("real terminal/error semantics loosened")
	}
}
