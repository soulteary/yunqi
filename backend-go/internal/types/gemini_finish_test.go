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
