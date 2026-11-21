package responses

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/soulteary/yunqi/internal/redaction"
)

func TestWebSocketErrorsAndCloseReasonsRedactCurrentCredentials(t *testing.T) {
	const secret = "opaque!websocket-credential=secret"
	redaction.Configure([]string{secret})
	defer redaction.Configure(nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := responsesWebSocketUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		closeWithWebSocketError(conn, 502, "upstream_error", secret+" https://alice:password@example.test")
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || strings.Contains(string(data), "alice") || !strings.Contains(string(data), redaction.Mask) {
		t.Fatalf("unsafe error message: %s", data)
	}
	_, _, err = conn.ReadMessage()
	closeErr, ok := err.(*websocket.CloseError)
	if !ok {
		t.Fatalf("expected close error: %v", err)
	}
	if strings.Contains(closeErr.Text, secret) || strings.Contains(closeErr.Text, "alice") || !strings.Contains(closeErr.Text, redaction.Mask) {
		t.Fatalf("unsafe close reason: %s", closeErr.Text)
	}
}

func TestRedactedWebSocketCloseReasonRemainsValidUTF8(t *testing.T) {
	reason := safeWebSocketCloseReason("x" + strings.Repeat("界", 100))
	if len(reason) > 123 || !utf8.ValidString(reason) {
		t.Fatalf("invalid close reason: %q", reason)
	}
}
