package gemini

import (
	"github.com/soulteary/yunqi/internal/handlers/common"
	"testing"
)

func TestGeminiTransportLinesDoNotCountAsFirstContent(t *testing.T) {
	observer := &common.StreamTimeoutObserver{}
	reader := &streamLineReader{observer: observer}
	for _, line := range []string{": keepalive", "event: response.created", `data: {"type":"response.in_progress"}`, `data: {"usageMetadata":{"promptTokenCount":4}}`} {
		reader.markActivity(line)
		if observer.HasFirstContent() {
			t.Fatalf("transport line ended TTFT: %s", line)
		}
	}
}
