package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soulteary/yunqi/internal/handlers/responses"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestResponsesWebSocketPlainGetReturns405(t *testing.T) {

	router := httpbridge.NewApp()
	handler := responses.WebSocketHandler(nil, nil, nil, nil)
	router.Get("/v1/responses", handler)
	router.Get("/:routePrefix/v1/responses", handler)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "root_plain_get_returns_405",
			path:       "/v1/responses",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "prefixed_plain_get_returns_405",
			path:       "/minimax/v1/responses",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)

			w := httptest.NewRecorder()
			httpbridge.Handler(router).ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}
