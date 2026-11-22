package config

import (
	"math"
	"os"
	"testing"
)

func TestEnvGetterContracts(t *testing.T) {
	const key = "YUNQI_TEST_ENV_GETTER_CONTRACT"
	cases := []struct {
		name, raw string
		unset     bool
		text      string
		integer   int
		integer64 int64
		floating  float64
	}{
		{name: "unset", unset: true, text: "fallback", integer: 17, integer64: 19, floating: 0.7},
		{name: "empty", text: "fallback", integer: 17, integer64: 19, floating: 0.7},
		{name: "whitespace", raw: " 2 ", text: " 2 ", integer: 17, integer64: 19, floating: 0.7},
		{name: "zero", raw: "0", text: "0"},
		{name: "signed", raw: "+12", text: "+12", integer: 12, integer64: 12, floating: 12},
		{name: "negative", raw: "-3", text: "-3", integer: -3, integer64: -3, floating: -3},
		{name: "fraction", raw: "0.25", text: "0.25", integer: 17, integer64: 19, floating: 0.25},
		{name: "exponent", raw: "1e2", text: "1e2", integer: 17, integer64: 19, floating: 100},
		{name: "invalid", raw: "false", text: "false", integer: 17, integer64: 19, floating: 0.7},
		{name: "integer overflow", raw: "9223372036854775808", text: "9223372036854775808", integer: 17, integer64: 19, floating: 9223372036854775808},
		{name: "float overflow", raw: "1e1000", text: "1e1000", integer: 17, integer64: 19, floating: 0.7},
		{name: "nan", raw: "NaN", text: "NaN", integer: 17, integer64: 19, floating: math.NaN()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(key, tc.raw)
			if tc.unset {
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
			}
			if got := getEnv(key, "fallback"); got != tc.text {
				t.Errorf("string = %q, want %q", got, tc.text)
			}
			if got := getEnvAsInt(key, 17); got != tc.integer {
				t.Errorf("int = %d, want %d", got, tc.integer)
			}
			if got := getEnvAsInt64(key, 19); got != tc.integer64 {
				t.Errorf("int64 = %d, want %d", got, tc.integer64)
			}
			if got := getEnvAsFloat(key, 0.7); got != tc.floating && !(math.IsNaN(got) && math.IsNaN(tc.floating)) {
				t.Errorf("float = %v, want %v", got, tc.floating)
			}
		})
	}
}

func TestNewEnvConfigParsingContracts(t *testing.T) {
	for _, tc := range []struct {
		name, enabled, disabled   string
		wantEnabled, wantDisabled bool
	}{
		{"empty defaults", "", "", false, true},
		{"literal values", "true", "false", true, false},
		{"uppercase", "TRUE", "FALSE", false, true},
		{"numeric aliases", "1", "0", false, true},
		{"surrounding whitespace", " true ", " false ", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ENABLE_HTTPS", tc.enabled)
			t.Setenv("ENABLE_WEB_UI", tc.disabled)
			t.Setenv("ENV", "")
			t.Setenv("NODE_ENV", "production")
			t.Setenv("MAX_REQUEST_BODY_SIZE_MB", "3")
			t.Setenv("SERVER_READ_TIMEOUT", "1")
			t.Setenv("METRICS_RETENTION_DAYS", "999")
			t.Setenv("METRICS_FAILURE_THRESHOLD", "0.25")
			cfg := NewEnvConfig()
			if cfg.EnableHTTPS != tc.wantEnabled || cfg.EnableWebUI != tc.wantDisabled {
				t.Fatalf("boolean parsing: HTTPS=%v, web UI=%v", cfg.EnableHTTPS, cfg.EnableWebUI)
			}
			if cfg.Env != "production" {
				t.Errorf("ENV fallback = %q", cfg.Env)
			}
			if cfg.MaxRequestBodySize != 3*1024*1024 {
				t.Errorf("body size = %d", cfg.MaxRequestBodySize)
			}
			if cfg.ServerReadTimeout != 10000 || cfg.MetricsRetentionDays != 366 {
				t.Errorf("clamps: timeout=%d retention=%d", cfg.ServerReadTimeout, cfg.MetricsRetentionDays)
			}
			if cfg.MetricsFailureThreshold != 0.25 {
				t.Errorf("failure threshold = %v", cfg.MetricsFailureThreshold)
			}
			t.Setenv("ENV", "development")
			if got := NewEnvConfig().Env; got != "development" {
				t.Errorf("ENV precedence = %q", got)
			}
		})
	}
}
