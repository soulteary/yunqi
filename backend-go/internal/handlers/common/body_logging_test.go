package common

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
)

func bodyLoggingTestManager(t *testing.T) *config.ConfigManager {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	cm, err := config.NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cm.UpdateBodyLogging("test", "", config.BodyLoggingSettings{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cm.UpdateBodyLogging("test:cleanup", "", config.BodyLoggingSettings{}); _ = cm.Close() })
	return cm
}
func TestBodyLogsDefaultOffAndExplicitOptInDoesNotConsumeRequest(t *testing.T) {
	cm := bodyLoggingTestManager(t)
	old := log.Writer()
	var captured bytes.Buffer
	log.SetOutput(&captured)
	defer log.SetOutput(old)
	const prompt = "private prompt must not be recorded by default"
	env := &config.EnvConfig{Env: "development", EnableRequestLogs: true, EnableResponseLogs: true}
	c, _ := httpbridge.TestContext(httptest.NewRecorder())
	httpbridge.Request(c).Header.Set("Content-Type", "application/json")
	body := []byte(`{"model":"test","messages":[{"role":"user","content":"` + prompt + `"}]}`)
	LogOriginalRequest(c, body, env, "Chat")
	LogUpstreamResponseBody(c, body, env, "Chat")
	req := httptest.NewRequest(http.MethodPost, "https://example.test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	logRequestDetails(req, env, "Chat")
	if strings.Contains(captured.String(), prompt) {
		t.Fatal("default verbose logging persisted body content")
	}
	if data, err := io.ReadAll(req.Body); err != nil || !bytes.Equal(data, body) {
		t.Fatal("disabled logging consumed request")
	}
	if _, err := cm.UpdateBodyLogging("actor", "", config.BodyLoggingSettings{RequestBodies: true, MaxBytes: 512}); err != nil {
		t.Fatal(err)
	}
	captured.Reset()
	LogOriginalRequest(c, body, env, "Chat")
	if !strings.Contains(captured.String(), prompt) {
		t.Fatal("audited request body opt-in was not applied")
	}
	captured.Reset()
	LogUpstreamResponseBody(c, body, env, "Chat")
	if strings.Contains(captured.String(), prompt) {
		t.Fatal("request opt-in enabled response body logging")
	}
}
func TestBodyLogsOmitMediaPersonalFieldsAndBoundWholeRecord(t *testing.T) {
	cm := bodyLoggingTestManager(t)
	if _, err := cm.UpdateBodyLogging("actor", "", config.BodyLoggingSettings{ResponseBodies: true, MaxBytes: 256}); err != nil {
		t.Fatal(err)
	}
	body := `{"email":"private@example.test","inlineData":{"data":"private-base64-bytes"},"model":"model","text":"` + strings.Repeat("界", 200) + `"}`
	safe := BodyTextForLog(body)
	if strings.Contains(safe, "private@example.test") || strings.Contains(safe, "private-base64-bytes") || len(safe) > 320 {
		t.Fatalf("unsafe/unbounded body log: %s", safe)
	}
	if !strings.Contains(safe, "truncated") {
		t.Fatal("bounded log omitted truncation feedback")
	}
	sse := BodyTextForLog("data: {\"data\":\"private-base64-bytes\"}\n\n")
	if strings.Contains(sse, "private-base64-bytes") {
		t.Fatal("SSE media escaped field minimization")
	}
}

func TestBodyLogPolicyOmitsReflectedPromptFromFailureRecords(t *testing.T) {
	_ = bodyLoggingTestManager(t)
	const prompt = "sensitive business prompt reflected by upstream"
	body := `{"error":{"type":"invalid_request_error","code":"invalid_request_error","message":"` + prompt + `"}}`
	if summary := errorBodySummaryForLog("Chat", 400, []byte(body)); strings.Contains(summary, prompt) || !strings.Contains(summary, "status=400") {
		t.Fatalf("unsafe error summary: %s", summary)
	}
	store := metrics.NewChannelLogStore()
	id := CreatePendingLog(store, "body-policy-failure", 0, "channel", "model", "", "", "", "local-test-key", "https://example.test", "Chat", "", metrics.RequestSourceProxy, nil, "")
	CompleteLog(store, "body-policy-failure", id, 400, false, body, false)
	if logs := store.Get("body-policy-failure"); len(logs) != 1 || strings.Contains(logs[0].ErrorInfo, prompt) || !strings.Contains(logs[0].ErrorInfo, "status=400") {
		t.Fatalf("failure record retained body: %+v", logs)
	}
	rejected := &UpstreamRequestRejectedError{Type: "invalid_request_error", Message: prompt}
	if strings.Contains(rejected.LogString("Responses"), prompt) || !strings.Contains(rejected.Error(), prompt) {
		t.Fatal("logging privacy changed client rejection data or retained prompt")
	}
}

func TestMinimizedBlacklistEvidenceRetainsOriginalQuotaReset(t *testing.T) {
	_ = bodyLoggingTestManager(t)
	body := []byte(`{"error":{"message":"private prompt; quota reset at 2030-02-03T04:05:06Z"}}`)
	result := ShouldBlacklistKey(402, body)
	message, recoverAt := blacklistEvidenceForLog("Chat", 402, body, result)
	if !result.ShouldBlacklist || recoverAt != "2030-02-03T04:05:06Z" || strings.Contains(message, "private prompt") {
		t.Fatalf("privacy changed quota lifecycle or retained body: message=%q recoverAt=%q", message, recoverAt)
	}
}
