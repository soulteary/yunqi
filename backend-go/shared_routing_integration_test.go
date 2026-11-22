package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/entrylease"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/keypool"
	"github.com/soulteary/yunqi/internal/ratelimit"
	"github.com/soulteary/yunqi/internal/redisconn"
)

func sharedRoutingNode(t *testing.T, endpoint, namespace string) *application {
	t.Helper()
	client, err := redisconn.New(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	epoch := strings.Repeat("c", 64)
	if err = client.Set(context.Background(), namespace+":{routing}:epoch", epoch, 0).Err(); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	a := redisAdmissionNode(t, endpoint, namespace, "", "route-proxy", 0, 0)
	manager, err := entrylease.NewRouting(endpoint, namespace, epoch, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	a.envCfg.ProxyRedisSharedRouting = true
	a.envCfg.SharedRoutingAdmission = manager
	a.channelScheduler.GetTraceAffinityManager().SetSharedStrict(manager)
	return a
}
func TestSharedRoutingRealHTTPChannelCapacityAcrossIndependentNodes(t *testing.T) {
	endpoint, _ := newIsolatedRedisEndpoint(t)
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
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
	defer upstream.Close()
	defer unblock()
	nodes := []*application{sharedRoutingNode(t, endpoint, "channel-http"), sharedRoutingNode(t, endpoint, "channel-http")}
	for _, node := range nodes {
		channel := redisAdmissionUpstream(upstream.URL, "openai")
		channel.RateLimitMaxConcurrent = 1
		channel.RateLimitRPM = 2
		channel.RateLimitWindowMinutes = 120
		if err := node.cfgManager.AddChatUpstream(channel); err != nil {
			t.Fatal(err)
		}
		manager := ratelimit.NewManager()
		manager.GetOrCreate("Chat", 0, ratelimit.Config{RPM: 2, WindowSeconds: 120, MaxConcurrent: 1})
		node.rateLimitManager = manager
		node.channelScheduler.SetRateLimitManager(manager)
		t.Cleanup(manager.Stop)
	}
	handlers := []http.Handler{httpbridge.Handler(nodes[0].newRouter()), httpbridge.Handler(nodes[1].newRouter())}
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- callRedisAdmission(handlers[0], "route-proxy", redisAdmissionBody) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first request missing")
	}
	for i := 0; i < 5; i++ {
		rejected := callRedisAdmission(handlers[1], "route-proxy", redisAdmissionBody)
		if rejected.Code != 429 || calls.Load() != 1 || rejected.Header().Get("Retry-After") == "" {
			t.Fatalf("capacity rejection %d: status=%d calls=%d %s", i, rejected.Code, calls.Load(), rejected.Body.String())
		}
	}
	// Rejected calls must not spend a second, non-refundable local RPM budget,
	// including channel defaults inherited by the key/quota scope.
	manager := nodes[1].rateLimitManager
	if used := manager.Get("Chat", 0).Status(time.Now()).WindowSize; used != 0 {
		t.Fatalf("rejections spent local channel RPM: %d", used)
	}
	scope := keypool.LimiterScopeFor("fake-provider", config.APIKeyConfig{})
	if scoped := manager.GetScoped("Chat", 0, scope); scoped != nil && scoped.Status(time.Now()).WindowSize != 0 {
		t.Fatal("rejections spent inherited local key RPM")
	}

	unblock()
	if response := <-first; response.Code != 200 {
		t.Fatalf("first request: %d %s", response.Code, response.Body.String())
	}
	if response := callRedisAdmission(handlers[1], "route-proxy", redisAdmissionBody); response.Code != 200 {
		t.Fatalf("released slot: %d %s", response.Code, response.Body.String())
	}
	if response := callRedisAdmission(handlers[0], "route-proxy", redisAdmissionBody); response.Code != 429 || calls.Load() != 2 {
		t.Fatalf("shared RPM reset by release: %d calls=%d", response.Code, calls.Load())
	}
}
func TestSharedRoutingStrictFirstRequestsBeforeAnyHTTPResponse(t *testing.T) {
	endpoint, _ := newIsolatedRedisEndpoint(t)
	entered := make(chan string, 8)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	provider := func(id string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			entered <- id
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, redisAdmissionReply)
		}))
	}
	a, b := provider("a"), provider("b")
	defer a.Close()
	defer b.Close()
	defer unblock()
	nodes := []*application{sharedRoutingNode(t, endpoint, "strict-http"), sharedRoutingNode(t, endpoint, "strict-http")}
	handlers := make([]http.Handler, 2)
	for i, node := range nodes {
		node.envCfg.StrictSessionAffinity = true
		for j, url := range []string{a.URL, b.URL} {
			channel := redisAdmissionUpstream(url, "openai")
			channel.ChannelUID = []string{"route-a", "route-b"}[j]
			channel.Name = channel.ChannelUID
			channel.Priority = 1 + (i+j)%2
			if err := node.cfgManager.AddChatUpstream(channel); err != nil {
				t.Fatal(err)
			}
		}
		handlers[i] = httpbridge.Handler(node.newRouter())
	}
	results := make(chan *httptest.ResponseRecorder, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(redisAdmissionBody))
			req.Header.Set("x-api-key", "route-proxy")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Session_id", "same-session")
			response := httptest.NewRecorder()
			handlers[i%2].ServeHTTP(response, req)
			results <- response
		}(i)
	}
	winner := ""
	for i := 0; i < 8; i++ {
		select {
		case id := <-entered:
			if winner == "" {
				winner = id
			}
			if id != winner {
				t.Error("strict first requests split", winner, id)
			}
		case <-time.After(8 * time.Second):
			unblock()
			t.Fatal("requests did not reach upstream")
		}
	}
	unblock()
	for i := 0; i < 8; i++ {
		response := <-results
		if response.Code != 200 {
			t.Errorf("strict request %d %s", response.Code, response.Body.String())
		}
	}
	// Losing all shared state must reject rather than create a different binding.
	client, _ := redisconn.New(endpoint)
	defer client.Close()
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	response := callRedisAdmission(handlers[1], "route-proxy", redisAdmissionBody)
	// Without an explicit session the request is invalid independently of Redis.
	if response.Code != 400 {
		t.Fatalf("missing strict identity: %d", response.Code)
	}
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(redisAdmissionBody))
	req.Header.Set("x-api-key", "route-proxy")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Session_id", "same-session")
	response = httptest.NewRecorder()
	handlers[1].ServeHTTP(response, req)
	if response.Code != 503 {
		t.Fatalf("empty shared state recreated binding: %d %s", response.Code, response.Body.String())
	}
}
