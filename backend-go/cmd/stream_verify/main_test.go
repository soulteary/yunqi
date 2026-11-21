package main

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func streamFixture(input, delta string) string {
	return fmt.Sprintf("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":%s,\"cache_read_input_tokens\":128}}}\n\n", input) +
		"data: {\"type\":\"content_block_start\"}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hello\"}}\n\n" +
		fmt.Sprintf("data: {\"type\":\"message_delta\",\"usage\":%s}\n\n", delta) +
		"data: {\"type\":\"message_stop\"}\n\n"
}

func serveStream(t *testing.T, body string, compressed bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Accept") != "text/event-stream" || strings.Contains(r.Header.Get("Accept-Encoding"), "br") {
			t.Errorf("invalid stream/encoding negotiation: %v", r.Header)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if compressed {
			w.Header().Set("Content-Encoding", "gzip")
			writer := gzip.NewWriter(w)
			_, _ = writer.Write([]byte(body))
			_ = writer.Close()
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRunCLIReturnsFailureForInvalidStreamsAndComparisons(t *testing.T) {
	t.Setenv("PROXY_ACCESS_KEY", "test-key")
	t.Setenv("UPSTREAM_API_KEY", "test-key")
	valid := streamFixture("0", `{"output_tokens":7}`)
	proxy := serveStream(t, valid, false)
	for _, test := range []struct {
		name, body string
		want       int
	}{
		{"matching", valid, 0},
		{"different-content", strings.Replace(valid, "hello", "world", 1), 1},
		{"different-cache-usage", strings.Replace(valid, "128", "64", 1), 1},
		{"missing-stop", strings.Replace(valid, "data: {\"type\":\"message_stop\"}\n\n", "", 1), 1},
		{"missing-content-block", strings.Replace(valid, "data: {\"type\":\"content_block_start\"}\n\n", "", 1), 1},
		{"error-before-stop", strings.Replace(valid, "data: {\"type\":\"message_stop\"}", "data: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"busy\"}}\n\ndata: {\"type\":\"message_stop\"}", 1), 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := serveStream(t, test.body, false)
			if got := runCLI([]string{"-proxy", proxy.URL, "-upstream", upstream.URL, "-model", "test-model"}); got != test.want {
				t.Fatalf("exit=%d, want %d", got, test.want)
			}
		})
	}
	broken := serveStream(t, "data: not-json\n\n", false)
	if got := runCLI([]string{"-proxy", broken.URL, "-model", "test-model"}); got != 1 {
		t.Fatalf("invalid proxy stream exit=%d, want 1", got)
	}
}

func TestRunCLIValidatesBeforeSendingRequests(t *testing.T) {
	t.Setenv("PROXY_ACCESS_KEY", "")
	t.Setenv("UPSTREAM_API_KEY", "")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer server.Close()
	for _, args := range [][]string{
		{"-proxy", server.URL, "-model", "test-model"},
		{"-proxy", server.URL, "-proxy-key", "test-key"},
		{"-proxy", server.URL, "-proxy-key", "test-key", "-model", "test-model", "-upstream", "http://example.invalid"},
		{"-proxy", server.URL, "-proxy-key", "test-key", "-model", "test-model", "-upstream", "://invalid", "-upstream-key", "test-key"},
		{"-unknown-flag"},
	} {
		if got := runCLI(args); got == 0 {
			t.Errorf("invalid arguments accepted: %v", args)
		}
	}
	if got := runCLI([]string{"-h"}); got != 0 {
		t.Errorf("help exit=%d, want 0", got)
	}
	if requests != 0 {
		t.Fatalf("invalid arguments triggered %d requests", requests)
	}
}

func TestVerifyStreamReportsHTTPEncodingAndScannerErrors(t *testing.T) {
	for _, kind := range []string{"http-error", "encoding", "scanner-limit", "malformed-gzip"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case "http-error":
					w.WriteHeader(http.StatusUnauthorized)
				case "encoding":
					w.Header().Set("Content-Encoding", "br")
				case "scanner-limit":
					_, _ = w.Write([]byte("data: " + strings.Repeat("x", 1024*1024+1)))
				case "malformed-gzip":
					w.Header().Set("Content-Encoding", "gzip")
					_, _ = w.Write([]byte("not-gzip"))
				}
			}))
			defer server.Close()
			result := verifyStream("test", server.URL, "test-key", "test-model", "hello", false)
			if len(result.Errors) == 0 {
				t.Fatal("failed response reported success")
			}
		})
	}
}

func TestVerifyStreamMergesUsageAndAcceptsZeroInput(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		for _, delta := range []string{`{"output_tokens":7}`, `{"input_tokens":0,"output_tokens":7}`} {
			t.Run(fmt.Sprintf("gzip=%v/delta=%s", compressed, delta), func(t *testing.T) {
				server := serveStream(t, streamFixture("0", delta), compressed)
				result := verifyStream("test", server.URL+"/", "test-key", "test-model", "hello", false)
				if len(result.Errors) != 0 {
					t.Fatalf("valid cached stream rejected: %v", result.Errors)
				}
				if result.FinalUsage == nil || toInt(result.FinalUsage.InputTokens) != 0 || toInt(result.FinalUsage.OutputTokens) != 7 || toInt(result.FinalUsage.CacheReadInputTokens) != 128 {
					t.Fatalf("usage was not merged: %+v", result.FinalUsage)
				}
			})
		}
	}
}

func TestVerifyStreamParsesSSEDataFields(t *testing.T) {
	body := streamFixture("5", `{"output_tokens":7}`)
	body = strings.ReplaceAll(body, "data: ", "data:")
	body = strings.Replace(body, `data:{"type":"message_start","message":`, "data:{\"type\":\"message_start\",\n"+`data:"message":`, 1)
	server := serveStream(t, ": keep-alive\n\n"+body, false)
	result := verifyStream("test", server.URL, "test-key", "test-model", "hello", false)
	if len(result.Errors) != 0 || result.RawContent != "hello" || result.EventCount != 5 {
		t.Fatalf("valid SSE rejected: %+v", result)
	}
}

func TestVerifyStreamReportsReadErrors(t *testing.T) {
	body := streamFixture("5", `{"input_tokens":5,"output_tokens":7}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(body)+100))
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	result := verifyStream("test", server.URL, "test-key", "test-model", "hello", false)
	if len(result.Errors) == 0 {
		t.Fatal("truncated HTTP body reported success")
	}
}

func TestVerifyStreamRejectsInvalidURLWithoutPanic(t *testing.T) {
	defer func() {
		if err := recover(); err != nil {
			t.Errorf("invalid URL panicked: %v", err)
		}
	}()
	result := verifyStream("test", "://invalid", "test-key", "test-model", "hello", false)
	if len(result.Errors) == 0 {
		t.Fatal("invalid URL reported success")
	}
}

func TestVerifyStreamRejectsInvalidUsage(t *testing.T) {
	for _, input := range []string{"-1", "1.5", `"wrong"`, "null"} {
		t.Run(input, func(t *testing.T) {
			server := serveStream(t, streamFixture(input, `{"output_tokens":7}`), false)
			result := verifyStream("test", server.URL, "test-key", "test-model", "hello", false)
			if len(result.Errors) == 0 {
				t.Fatal("invalid usage reported success")
			}
		})
	}
}

func TestVerifyStreamRedactsAPIKeyInFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid key: test-secret-key"))
	}))
	defer server.Close()
	result := verifyStream("test", server.URL, "test-secret-key", "test-model", "hello", false)
	errors := strings.Join(result.Errors, "\n")
	if strings.Contains(errors, "test-secret-key") || !strings.Contains(errors, "[REDACTED]") {
		t.Fatalf("failed to redact credential: %s", errors)
	}
}
