package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestAPIModePrefixedProxyRoutesKeepProxyAuthentication(t *testing.T) {
	a := newRouteTestApplication(t, false, "production")
	handler := httpbridge.Handler(a.newRouter())
	endpoints := []struct{ method, path string }{
		{"POST", "/v1/messages"}, {"POST", "/v1/messages/count_tokens"},
		{"GET", "/v1/models"}, {"GET", "/v1/models/test"},
		{"POST", "/v1/responses"}, {"GET", "/v1/responses"},
		{"POST", "/v1/responses/compact"}, {"POST", "/v1/alpha/history/v2/items"},
		{"POST", "/v1beta/models/test:generateContent"}, {"POST", "/v1/chat/completions"},
		{"POST", "/v1/images/generations"}, {"POST", "/v1/images/edits"},
		{"POST", "/v1/images/variations"}, {"POST", "/v1/embeddings"},
	}
	for _, prefix := range []string{"", "/team", "/api", "/admin", "/api-team"} {
		for _, endpoint := range endpoints {
			t.Run(endpoint.method+prefix+endpoint.path, func(t *testing.T) {
				for _, key := range []string{"", "route-admin", "route-proxy"} {
					req := httptest.NewRequest(endpoint.method, prefix+endpoint.path, strings.NewReader("{}"))
					req.Header.Set("Content-Type", "application/json")
					if endpoint.method == http.MethodGet && endpoint.path == "/v1/responses" {
						// Reach WebSocket authentication without opening a socket: the
						// incomplete handshake is rejected only after key validation.
						req.Header.Set("Connection", "Upgrade")
						req.Header.Set("Upgrade", "websocket")
					}
					if key != "" {
						req.Header.Set("x-api-key", key)
					}
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, req)
					if key != "route-proxy" {
						if w.Code != http.StatusUnauthorized {
							t.Fatalf("key=%q status=%d, want 401: %s", key, w.Code, w.Body.String())
						}
					} else if w.Code == http.StatusUnauthorized || strings.Contains(w.Body.String(), "Web界面已禁用") {
						t.Fatalf("valid proxy key blocked before data-plane handler: %d %s", w.Code, w.Body.String())
					}
				}
			})
		}
	}
	for _, path := range []string{"/health", "/team/health", "/api/health", "/admin/health"} {
		t.Run("GET"+path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("health status=%d, want 200: %s", w.Code, w.Body.String())
			}
		})
	}
}
