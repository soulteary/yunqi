package common

import (
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/scheduler"
)

func TestApplyAgentModelProfileSetsMinimumContextWindow(t *testing.T) {
	requirement := &scheduler.ContextRequirement{
		InputTokens:       41808,
		OutputTokens:      8192,
		RequiredTokens:    50000,
		ExplicitOutputMax: false,
	}

	ApplyAgentModelProfile(requirement, "sonnet", config.Config{})

	if requirement.MinimumContextWindowTokens != 1000000 {
		t.Fatalf("MinimumContextWindowTokens = %d, want 1000000", requirement.MinimumContextWindowTokens)
	}
}
