package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gorilla/websocket"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/entrybudget"
	"github.com/soulteary/yunqi/internal/entrylease"
	"github.com/soulteary/yunqi/internal/handlers/common"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/middleware"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/warmup"
)

func redisAdmissionNode(t *testing.T, redisURL, namespace, budgetPath, key string, concurrency, rpm int) *application {
	t.Helper()
	a := newRouteTestApplication(t, true, "production")
	a.envCfg.RequestTimeout = 10000
	a.envCfg.ProxyMaxConcurrency = concurrency
	a.envCfg.ProxyRedisURL = redisURL
	a.envCfg.ProxyRedisMaxRPM = rpm
	manager, err := entrylease.New(redisURL, namespace, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	a.envCfg.ProxyRedisAdmission = manager
	t.Cleanup(func() { _ = manager.Close() })
	if err := a.cfgManager.PutEntryKey(config.EntryKey{ID: "shared-entry", KeyHash: config.EntryKeyHash(key), PrincipalID: "shared-team"}, ""); err != nil {
		t.Fatal(err)
	}
	a.envCfg.ManagedProxyKeyLookup = a.cfgManager.FindEntryKey
	if budgetPath != "" {
		a.envCfg.BudgetManager = entrybudget.NewManager(budgetPath)
		t.Cleanup(func() { _ = a.envCfg.BudgetManager.Close() })
		if err := a.cfgManager.PutPrincipalBudget(entrybudget.Policy{PrincipalID: "shared-team", Period: "lifetime", LimitNanoUSD: 1000000000, RequestCapNanoUSD: 1000000000}); err != nil {
			t.Fatal(err)
		}
	}
	a.sessionManager = session.NewSessionManager(time.Hour, 100, 100000)
	a.channelScheduler = scheduler.NewChannelScheduler(a.cfgManager, a.messagesMetricsManager, a.responsesMetricsManager, a.geminiMetricsManager, a.chatMetricsManager, a.imagesMetricsManager, session.NewTraceAffinityManager(), warmup.NewURLManager(time.Second, 1), a.vectorsMetricsManager)
	return a
}
func redisAdmissionUpstream(url, kind string) config.UpstreamConfig {
	price := 1.0
	return config.UpstreamConfig{ChannelUID: "shared-" + kind, Name: "shared-" + kind, BaseURL: url, APIKeys: []string{"fake-provider"}, ServiceType: kind, SupportedModels: []string{"shared-model"}, Status: "active", ModelCapabilities: map[string]config.UpstreamModelCapability{"shared-model": {MaxOutputTokens: 10, Pricing: &config.ModelPricing{InputCacheMissPrice: &price, OutputPrice: &price}}}}
}
func callRedisAdmission(handler http.Handler, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

const redisAdmissionBody = `{"model":"shared-model","max_tokens":10,"messages":[{"role":"user","content":"hello"}]}`
const redisAdmissionReply = `{"id":"chat","object":"chat.completion","model":"shared-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`

func TestRedisAdmissionTwoHTTPNodesAndBudgetsSharePrincipalAcrossKeyRotation(t *testing.T) {
	redisURL, _ := newIsolatedRedisEndpoint(t)
	db := filepath.Join(t.TempDir(), "budget.db")
	var calls atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, redisAdmissionReply)
	}))
	defer provider.Close()
	defer unblock()
	first := redisAdmissionNode(t, redisURL, "http-shared", db, "route-proxy", 1, 0)
	second := redisAdmissionNode(t, redisURL, "http-shared", db, "rotated-key", 1, 0)
	for _, node := range []*application{first, second} {
		if err := node.cfgManager.AddChatUpstream(redisAdmissionUpstream(provider.URL, "openai")); err != nil {
			t.Fatal(err)
		}
	}
	firstHandler := httpbridge.Handler(first.newRouter())
	secondHandler := httpbridge.Handler(second.newRouter())
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- callRedisAdmission(firstHandler, "route-proxy", redisAdmissionBody) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first upstream did not start")
	}
	rejected := callRedisAdmission(secondHandler, "rotated-key", redisAdmissionBody)
	if rejected.Code != 429 || calls.Load() != 1 {
		t.Fatalf("cross-node concurrency bypass: %d calls=%d %s", rejected.Code, calls.Load(), rejected.Body.String())
	}
	unblock()
	if response := <-result; response.Code != 200 {
		t.Fatalf("first request %d %s", response.Code, response.Body.String())
	}
	if response := callRedisAdmission(secondHandler, "rotated-key", redisAdmissionBody); response.Code != 200 {
		t.Fatalf("released slot %d %s", response.Code, response.Body.String())
	}
	store, _ := first.envCfg.BudgetManager.Store()
	state, err := store.Snapshot(context.Background(), entrybudget.Policy{PrincipalID: "shared-team", Period: "lifetime"}, time.Now())
	if err != nil || state.Reserved != 0 || state.Spent != 6000 {
		t.Fatalf("shared lifetime accounting %+v %v", state, err)
	}
	records, err := store.Requests(context.Background(), "shared-team", 100)
	if err != nil || len(records) != 2 {
		t.Fatalf("denied node reserved/double settled %+v %v", records, err)
	}
}

func TestRedisAdmissionRetainsDetachedBodyAndBudgetAfterHTTPCompletes(t *testing.T) {
	redisURL, _ := newIsolatedRedisEndpoint(t)
	db := filepath.Join(t.TempDir(), "budget.db")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, redisAdmissionReply)
	}))
	defer provider.Close()
	first := redisAdmissionNode(t, redisURL, "detached", db, "route-proxy", 1, 0)
	second := redisAdmissionNode(t, redisURL, "detached", db, "route-proxy", 1, 0)
	upstream := redisAdmissionUpstream(provider.URL, "openai")
	bodies := make(chan io.ReadCloser, 2)
	handlerFor := func(node *application) http.Handler {
		app := httpbridge.NewApp()
		app.Use(middleware.ProxyAdmissionMiddleware(node.envCfg))
		app.Use(middleware.ProxyBudgetMiddleware(node.envCfg, node.cfgManager))
		app.Post("/v1/chat/completions", func(c fiber.Ctx) error {
			ctx := context.WithoutCancel(httpbridge.Request(c).Context())
			request, err := http.NewRequestWithContext(ctx, "POST", provider.URL+"/v1/chat/completions", strings.NewReader(redisAdmissionBody))
			if err != nil {
				return err
			}
			response, err := common.SendRequest(request, &upstream, node.envCfg, false, "Chat")
			if err != nil {
				return err
			}
			bodies <- response.Body
			httpbridge.JSON(c, 200, map[string]bool{"accepted": true})
			return nil
		})
		return httpbridge.Handler(app)
	}
	a, b := handlerFor(first), handlerFor(second)
	if response := callRedisAdmission(a, "route-proxy", redisAdmissionBody); response.Code != 200 {
		t.Fatalf("detached request %d %s", response.Code, response.Body.String())
	}
	body := <-bodies
	defer body.Close()
	store, _ := first.envCfg.BudgetManager.Store()
	policy := entrybudget.Policy{PrincipalID: "shared-team", Period: "lifetime"}
	state, err := store.Snapshot(context.Background(), policy, time.Now())
	if err != nil || state.Reserved <= 0 || state.Spent != 0 {
		t.Fatalf("HTTP completion prematurely settled active body %+v %v", state, err)
	}
	if response := callRedisAdmission(b, "route-proxy", redisAdmissionBody); response.Code != 429 {
		t.Fatalf("detached body freed Redis slot %d %s", response.Code, response.Body.String())
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
	if response := callRedisAdmission(b, "route-proxy", redisAdmissionBody); response.Code != 200 {
		t.Fatalf("closed body did not release %d %s", response.Code, response.Body.String())
	}
	next := <-bodies
	_, _ = io.Copy(io.Discard, next)
	_ = next.Close()
	state, err = store.Snapshot(context.Background(), policy, time.Now())
	if err != nil || state.Spent != 6000 || state.Reserved != 0 {
		t.Fatalf("detached accounting not settled once %+v %v", state, err)
	}
}

func TestRedisLeaseLossDuringCommittedStreamKeepsProviderKeyHealthy(t *testing.T) {
	redisURL, stopRedis := newIsolatedRedisEndpoint(t)
	var calls atomic.Int64
	started := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"id\":\"chat\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n")
			w.(http.Flusher).Flush()
			close(started)
			select {
			case <-r.Context().Done():
				close(canceled)
			case <-release:
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, redisAdmissionReply)
	}))
	defer provider.Close()
	defer unblock()
	node := redisAdmissionNode(t, redisURL, "lost-stream", "", "route-proxy", 1, 0)
	if err := node.cfgManager.AddChatUpstream(redisAdmissionUpstream(provider.URL, "openai")); err != nil {
		t.Fatal(err)
	}
	handler := httpbridge.Handler(node.newRouter())
	result := make(chan *httptest.ResponseRecorder, 1)
	incoming, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(strings.Replace(redisAdmissionBody, `"max_tokens":10`, `"max_tokens":10,"stream":true`, 1))).WithContext(incoming)
	request.Header.Set("x-api-key", "route-proxy")
	request.Header.Set("Content-Type", "application/json")
	go func() { response := httptest.NewRecorder(); handler.ServeHTTP(response, request); result <- response }()
	completed := false
	defer func() {
		cancel()
		unblock()
		if !completed {
			select {
			case <-result:
			case <-time.After(3 * time.Second):
				t.Error("stream worker survived cleanup")
			}
		}
	}()
	select {
	case <-started:
	case <-time.After(4 * time.Second):
		t.Fatal("stream did not start")
	}
	stopRedis()
	select {
	case <-canceled:
	case <-time.After(7 * time.Second):
		t.Fatal("lease loss did not cancel actual upstream")
	}
	select {
	case response := <-result:
		completed = true
		if response.Code != 200 || !strings.Contains(response.Body.String(), "hello") {
			t.Fatalf("committed stream corrupted %d %s", response.Code, response.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("gateway stream did not finish after lease loss")
	}
	// Disable the unavailable optional backend after all active work has ended.
	// The same single provider key must still be usable without a cooldown.
	node.envCfg.ProxyRedisURL = ""
	if response := callRedisAdmission(handler, "route-proxy", redisAdmissionBody); response.Code != 200 {
		t.Fatalf("lease failure poisoned provider key %d %s", response.Code, response.Body.String())
	}
	if calls.Load() != 2 {
		t.Fatalf("lease loss retried or failed to recover provider: %d", calls.Load())
	}
}

func readRedisWebSocketEvent(t *testing.T, conn *websocket.Conn, kind string, status int) map[string]interface{} {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(6 * time.Second))
	for i := 0; i < 20; i++ {
		var event map[string]interface{}
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event["type"] == kind && (status == 0 || event["status_code"] == float64(status)) {
			return event
		}
	}
	t.Fatalf("missing WS event %s status=%d", kind, status)
	return nil
}
func TestRedisWebSocketPerTurnAdmissionControlRPMAndBudgetLifecycle(t *testing.T) {
	redisURL, _ := newIsolatedRedisEndpoint(t)
	db := filepath.Join(t.TempDir(), "ws-budget.db")
	var calls atomic.Int64
	firstCanceled := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, fmt.Sprintf("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_%d\"}}\n\n", count))
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\" world\"}\n\n")
		w.(http.Flusher).Flush()
		if count == 1 {
			<-r.Context().Done()
			close(firstCanceled)
			return
		}
		_, _ = io.WriteString(w, fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_%d\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n", count))
	}))
	defer provider.Close()
	first := redisAdmissionNode(t, redisURL, "ws-shared", db, "route-proxy", 1, 2)
	second := redisAdmissionNode(t, redisURL, "ws-shared", db, "rotated-key", 1, 2)
	for _, node := range []*application{first, second} {
		if err := node.cfgManager.AddResponsesUpstream(redisAdmissionUpstream(provider.URL, "responses")); err != nil {
			t.Fatal(err)
		}
	}
	firstServer := httptest.NewServer(httpbridge.Handler(first.newRouter()))
	defer firstServer.Close()
	secondServer := httptest.NewServer(httpbridge.Handler(second.newRouter()))
	defer secondServer.Close()
	dial := func(serverURL, key string) *websocket.Conn {
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(serverURL, "http")+"/v1/responses", http.Header{"x-api-key": []string{key}})
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}
	a, b := dial(firstServer.URL, "route-proxy"), dial(secondServer.URL, "rotated-key")
	defer a.Close()
	defer b.Close()
	create := map[string]interface{}{"type": "response.create", "model": "shared-model", "input": "hello", "max_output_tokens": 10}
	if err := a.WriteJSON(create); err != nil {
		t.Fatal(err)
	}
	readRedisWebSocketEvent(t, a, "response.output_text.delta", 0)
	if err := b.WriteJSON(create); err != nil {
		t.Fatal(err)
	}
	readRedisWebSocketEvent(t, b, "error", 429)
	if calls.Load() != 1 {
		t.Fatalf("WS cross-node slot bypassed %d", calls.Load())
	}
	if err := a.WriteJSON(map[string]interface{}{"type": "response.cancel", "response_id": "resp_1"}); err != nil {
		t.Fatal(err)
	}
	readRedisWebSocketEvent(t, a, "response.cancelled", 0)
	select {
	case <-firstCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("WS cancel did not stop actual provider")
	}
	if err := b.WriteJSON(create); err != nil {
		t.Fatal(err)
	}
	readRedisWebSocketEvent(t, b, "response.completed", 0)
	// An idle control may return its own 400; it must not consume the second
	// admitted call's RPM. A third create is rejected at the actual 2/min limit.
	if err := b.WriteJSON(map[string]interface{}{"type": "response.cancel"}); err != nil {
		t.Fatal(err)
	}
	readRedisWebSocketEvent(t, b, "error", 400) // One pending-frame queue; wait for control acknowledgement.
	if err := b.WriteJSON(create); err != nil {
		t.Fatal(err)
	}
	readRedisWebSocketEvent(t, b, "error", 429)
	if calls.Load() != 2 {
		t.Fatalf("WS control/handshake/denial counted or turn bypassed: %d", calls.Load())
	}
	store, _ := first.envCfg.BudgetManager.Store()
	records, err := store.Requests(context.Background(), "shared-team", 100)
	if err != nil || len(records) != 2 {
		t.Fatalf("WS controls/denied turns created budget entries %+v %v", records, err)
	}
}
