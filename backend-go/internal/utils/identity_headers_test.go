package utils

import (
	"github.com/soulteary/yunqi/internal/httpbridge"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIdentityAssertionsNeverReachUpstreamOrClient(t *testing.T) {
	c, _ := httpbridge.TestContext(httptest.NewRecorder())
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("X-Yunqi-Identity-Secret", "shared-secret")
	req.Header.Set("X-Yunqi-Identity-Subject", "person")
	req.Header.Set("X-Yunqi-Management-Request", "1")
	httpbridge.SetRequest(c, req)
	headers := PrepareUpstreamHeaders(c, "upstream.example")
	ApplyCustomHeaders(headers, map[string]string{"X-Yunqi-Identity-Secret": "custom-secret", "X-Allowed": "yes"})
	ApplyCustomHeadersProtected(headers, map[string]string{"X-Yunqi-Identity-Session": "session", "X-Yunqi-Management-Request": "1"}, nil)
	for name := range headers {
		if strings.HasPrefix(strings.ToLower(name), "x-yunqi-identity-") || strings.EqualFold(name, "X-Yunqi-Management-Request") {
			t.Fatalf("identity assertion forwarded: %s", name)
		}
	}
	if headers.Get("X-Allowed") != "yes" {
		t.Fatal("unrelated custom header removed")
	}
	w := httptest.NewRecorder()
	ForwardResponseHeaders(http.Header{"X-Yunqi-Identity-Secret": []string{"secret"}, "X-Allowed": []string{"yes"}}, w)
	if w.Header().Get("X-Yunqi-Identity-Secret") != "" || w.Header().Get("X-Allowed") != "yes" {
		t.Fatal("upstream response spoofed identity header")
	}
}
