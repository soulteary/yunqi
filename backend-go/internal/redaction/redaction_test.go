package redaction

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const oddKey = "vendor!opaque~credential=with+symbols/2026"

func TestKnownCredentialsJWTAndUserinfo(t *testing.T) {
	Configure([]string{oddKey, "xy"})
	defer Configure(nil)
	input := oddKey + " https://alice:secret@private.example/path Bearer opaque-token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SignatureLongEnough xy"
	got := String(input)
	for _, secret := range []string{oddKey, "alice", "secret@", "opaque-token", "eyJhbGciOiJIUzI1NiJ9", " xy"} {
		if strings.Contains(got, secret) {
			t.Fatalf("leaked %q in %s", secret, got)
		}
	}
	if got := String("syntax says xylophone"); got != "syntax says xylophone" {
		t.Fatalf("short key corrupted ordinary text: %q", got)
	}
}
func TestJSONKeepsSchemaAndIntegerPrecision(t *testing.T) {
	Configure([]string{oddKey})
	defer Configure(nil)
	data := []byte(`{"id":9007199254740993,"error":{"message":"` + oddKey + `","authorization":"unrecognised credential"},"type":"error"}`)
	safe := JSON(data)
	if !json.Valid(safe) || !bytes.Contains(safe, []byte("9007199254740993")) || bytes.Contains(safe, []byte(oddKey)) || bytes.Contains(safe, []byte("unrecognised credential")) {
		t.Fatalf("unsafe JSON: %s", safe)
	}
	success := []byte(`{"type":"response.output_text.delta","delta":"` + oddKey + `"}`)
	if !bytes.Equal(ErrorJSON(success), success) {
		t.Fatal("successful model output was altered")
	}
}
func TestObserveCurrentAndAttemptedCredentials(t *testing.T) {
	Configure(nil)
	defer Configure(nil)
	ObserveJSON([]byte(`{"chatUpstream":[{"apiKeys":["unknown-vendor-current"]}],"managedAccounts":[{"credentials":[{"access_token":"unknown-managed-token"}]}]}`))
	req, _ := http.NewRequest("GET", "https://user:proxy-password@example.test?key=unsaved-query-key", nil)
	req.Header.Set("Authorization", "Bearer unsaved-candidate-key")
	ObserveRequest(req)
	for _, value := range []string{"unknown-vendor-current", "unknown-managed-token", "proxy-password", "unsaved-query-key", "unsaved-candidate-key"} {
		if strings.Contains(String(value), value) {
			t.Fatalf("credential was not observed: %q", value)
		}
	}
}
func TestSSEErrorRedactionPreservesSuccessfulEvents(t *testing.T) {
	Configure([]string{oddKey})
	defer Configure(nil)
	event := "event: error\ndata: {\"message\":\"" + oddKey + "\"}\n\n"
	if strings.Contains(SSEErrorEvent(event), oddKey) {
		t.Fatal("explicit SSE error leaked")
	}
	event = "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"" + oddKey + "\"}}}\n\n"
	if strings.Contains(SSEErrorEvent(event), oddKey) {
		t.Fatal("Responses failed event leaked")
	}
	success := "data: {\"choices\":[{\"delta\":{\"content\":\"" + oddKey + "\"}}]}\n\n"
	if got := SSEErrorEvent(success); got != success {
		t.Fatal("normal content event was altered")
	}
	r := &SSELineRedactor{}
	r.Line("event: error")
	if strings.Contains(r.Line("data: {\"message\":\""+oddKey+"\"}"), oddKey) {
		t.Fatal("separately written error event leaked")
	}
}
func TestWriterReportsOriginalLength(t *testing.T) {
	Configure([]string{oddKey})
	defer Configure(nil)
	var buffer bytes.Buffer
	n, err := Writer(&buffer).Write([]byte(oddKey))
	if err != nil || n != len(oddKey) || buffer.String() != Mask {
		t.Fatalf("writer n=%d err=%v data=%q", n, err, buffer.String())
	}
}
func TestConcurrentCredentialObservation(t *testing.T) {
	Configure(nil)
	defer Configure(nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				ObserveJSON([]byte(`{"apiKeys":["` + oddKey + `"]}`))
				_ = String(oddKey)
			}
		}()
	}
	wg.Wait()
	if String(oddKey) != Mask {
		t.Fatal("current credential was not retained")
	}
}

func TestCredentialEncodedFormsAreRedacted(t *testing.T) {
	const key = "vendor+Opaque/Key<&>"
	Configure([]string{key})
	defer Configure(nil)
	quoted, _ := json.Marshal(key)
	for _, value := range []string{url.QueryEscape(key), url.PathEscape(key), string(quoted[1 : len(quoted)-1])} {
		if String(value) != Mask {
			t.Fatalf("encoded credential leaked: %q", String(value))
		}
	}
}

func TestDiagnosticJSONWithoutSecretsPreservesExactBytes(t *testing.T) {
	Configure(nil)
	defer Configure(nil)
	data := []byte(`{ "error": { "type": "invalid_request_error", "message": "invalid request parameter" } }`)
	if !bytes.Equal(JSON(data), data) {
		t.Fatalf("unmodified diagnostic bytes changed: %s", JSON(data))
	}
}

func TestMixedSSEBatchOnlyRedactsErrorEvent(t *testing.T) {
	Configure([]string{oddKey})
	defer Configure(nil)
	success := "data: {\"choices\":[{\"delta\":{\"content\":\"" + oddKey + "\"}}]}\n\n"
	failure := "event: error\ndata: {\"message\":\"" + oddKey + "\"}\n\n"
	safe := SSEErrorEvent(success + failure)
	if !strings.HasPrefix(safe, success) || strings.Count(safe, oddKey) != 1 || !strings.Contains(safe, Mask) {
		t.Fatalf("mixed stream semantics changed: %s", safe)
	}
}
