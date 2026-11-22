package config

import "testing"

func TestUsageDrainTimeoutDefaultsOffAndClamps(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{{"", 0}, {"-1", 0}, {"50", 50}, {"999999", 5000}} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("USAGE_DRAIN_TIMEOUT_MS", tc.value)
			if got := NewEnvConfig().UsageDrainTimeoutMs; got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}
