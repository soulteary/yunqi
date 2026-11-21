package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/handlers"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/httpclient"
)

func TestNetworkPolicyReachesSixModelHandlers(t *testing.T) {
	var proxyCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyCalls.Add(1); w.WriteHeader(502) }))
	defer proxy.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"test-model"}],"models":[{"name":"models/test-model","supportedGenerationMethods":["generateContent"]}]}`))
	}))
	defer upstream.Close()
	a := newRouteTestApplication(t, true, "production")
	router := httpbridge.Handler(a.newRouter())
	before, _ := json.Marshal(a.cfgManager.GetConfig())
	for _, kind := range []string{"messages", "chat", "responses", "gemini", "images", "vectors"} {
		t.Run(kind, func(t *testing.T) {
			body, _ := json.Marshal(map[string]interface{}{"baseUrl": upstream.URL, "key": "test-only", "proxyUrl": proxy.URL, "proxyPreferDirect": true, "outboundNetwork": map[string]string{"proxyMode": "direct"}})
			req := httptest.NewRequest("POST", "/api/"+kind+"/channels/0/models", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-api-key", "route-admin")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "test-model") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			body, _ = json.Marshal(map[string]interface{}{"baseUrl": upstream.URL, "key": "test-only", "proxyUrl": proxy.URL, "proxyPreferDirect": true, "outboundNetwork": map[string]interface{}{"proxyMode": "direct_first", "interfaceName": "yunqi-missing", "dns": map[string]interface{}{"mode": "bound", "servers": []string{"127.0.0.1:53"}}}})
			req = httptest.NewRequest("POST", "/api/"+kind+"/channels/0/models", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-api-key", "route-admin")
			w = httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != 503 || !strings.Contains(w.Body.String(), "network_egress_unavailable") {
				t.Fatalf("local error lost: status=%d body=%s", w.Code, w.Body.String())
			}

		})
	}
	if proxyCalls.Load() != 0 {
		t.Fatalf("force direct sent %d proxy requests", proxyCalls.Load())
	}
	after, _ := json.Marshal(a.cfgManager.GetConfig())
	if string(before) != string(after) {
		t.Fatal("temporary tests persisted configuration")
	}
}
func TestNetworkDiagnosisIsExplicitAndDoesNotSendHTTP(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer upstream.Close()
	a := newRouteTestApplication(t, true, "production")
	router := httpbridge.Handler(a.newRouter())
	before, _ := json.Marshal(a.cfgManager.GetConfig())
	req := httptest.NewRequest("POST", "/api/network/diagnose", strings.NewReader(`{"baseUrl":"`+upstream.URL+`","outboundNetwork":{"proxyMode":"direct"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "route-admin")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"success":true`) || calls.Load() != 0 {
		t.Fatalf("status=%d body=%s HTTP calls=%d", w.Code, w.Body.String(), calls.Load())
	}
	after, _ := json.Marshal(a.cfgManager.GetConfig())
	if string(before) != string(after) {
		t.Fatal("diagnosis persisted configuration")
	}
}

func TestChannelModelsAdapterPreservesLocalEgressError(t *testing.T) {
	fetch := channelModelsHandlerFetcher(func(c fiber.Ctx) error {
		common.RespondNetworkEgressError(c, &httpclient.EgressError{Stage: "dns", Err: errors.New("unavailable")})
		return nil
	})
	_, err := fetch(context.Background(), handlers.DiscoveryModelsFetchRequest{BaseURL: "https://example.com"})
	if !httpclient.IsEgressError(err) {
		t.Fatalf("local error type lost in health/discovery adapter: %v", err)
	}
}
