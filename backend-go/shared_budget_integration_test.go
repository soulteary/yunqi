package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/entrybudget"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/redisconn"
)

func TestSharedBudgetIndependentHTTPNodesCannotDoubleSpend(t *testing.T) {
	endpoint, stopRedis := newIsolatedRedisEndpoint(t)
	ctx := context.Background()
	bootstrap, err := redisconn.New(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	if err := entrybudget.InitializeRedis(ctx, bootstrap, "http-budget", "epoch-v1"); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, redisAdmissionReply)
	}))
	defer provider.Close()
	defer unblock()
	nodes := []*application{redisAdmissionNode(t, endpoint, "http-budget-admission", "", "route-proxy", 0, 0), redisAdmissionNode(t, endpoint, "http-budget-admission", "", "rotated-key", 0, 0)}
	p := entrybudget.Policy{PrincipalID: "shared-team", Period: "lifetime", LimitNanoUSD: 1000000000, RequestCapNanoUSD: 1000000000}
	for _, node := range nodes {
		client, err := redisconn.New(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		node.envCfg.BudgetManager, err = entrybudget.NewRedisManager(client, "http-budget", "epoch-v1")
		if err != nil {
			t.Fatal(err)
		}
		manager := node.envCfg.BudgetManager
		t.Cleanup(func() { manager.Close() })
		if err := node.cfgManager.PutPrincipalBudget(p); err != nil {
			t.Fatal(err)
		}
		if err := node.cfgManager.AddChatUpstream(redisAdmissionUpstream(provider.URL, "openai")); err != nil {
			t.Fatal(err)
		}
	}
	first, second := httpbridge.Handler(nodes[0].newRouter()), httpbridge.Handler(nodes[1].newRouter())
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- callRedisAdmission(first, "route-proxy", redisAdmissionBody) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream did not start")
	}
	store, _ := nodes[1].envCfg.BudgetManager.Store()
	snapshot, err := store.Snapshot(ctx, p, time.Now())
	if err != nil || snapshot.Reserved <= 3000 {
		t.Fatalf("reservation=%+v %v", snapshot, err)
	}
	// Tighten the same policy while an old request remains in flight. The next
	// request must account for the reservation made by the OTHER HTTP instance.
	p.LimitNanoUSD = snapshot.Reserved + 3000
	for _, node := range nodes {
		if err := node.cfgManager.PutPrincipalBudget(p); err != nil {
			t.Fatal(err)
		}
	}
	rejected := callRedisAdmission(second, "rotated-key", redisAdmissionBody)
	if rejected.Code != 429 || calls.Load() != 1 {
		t.Fatalf("double spend status=%d calls=%d body=%s", rejected.Code, calls.Load(), rejected.Body.String())
	}
	unblock()
	if response := <-done; response.Code != 200 {
		t.Fatalf("first=%d %s", response.Code, response.Body.String())
	}
	if response := callRedisAdmission(second, "rotated-key", redisAdmissionBody); response.Code != 200 {
		t.Fatalf("settled reservation unavailable=%d %s", response.Code, response.Body.String())
	}
	snapshot, err = store.Snapshot(ctx, p, time.Now())
	if err != nil || snapshot.Spent != 6000 || snapshot.Reserved != 0 {
		t.Fatalf("cross-node settlement=%+v %v", snapshot, err)
	}
	stopRedis()
	denied := callRedisAdmission(second, "rotated-key", redisAdmissionBody)
	if denied.Code != 503 || calls.Load() != 2 {
		t.Fatalf("offline backend fell back to local ledger: status=%d calls=%d", denied.Code, calls.Load())
	}
}
