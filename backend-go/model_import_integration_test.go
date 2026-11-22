package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/session"
	"github.com/soulteary/yunqi/internal/warmup"
)

func TestModelImportRealProxyAndModelsCacheBoundary(t *testing.T) {
	var offline atomic.Bool
	var calls atomic.Int64
	var actual atomic.Value
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/models") {
			if offline.Load() {
				w.WriteHeader(503)
				return
			}
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"provider/GPT-raw","object":"model"},{"id":"provider/OTHER","object":"model"}]}`))
			return
		}
		if strings.Contains(r.URL.Path, "/models/") {
			calls.Add(1)
			actual.Store(r.URL.Path)
			_, _ = w.Write([]byte(`{"id":"provider/GPT-raw","object":"model"}`))
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		actual.Store(body.Model)
		calls.Add(1)
		_, _ = w.Write([]byte(`{"id":"chat","object":"chat.completion","model":"provider/GPT-raw","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`))
	}))
	defer provider.Close()
	a := newRouteTestApplication(t, true, "production")
	a.envCfg.RequestTimeout = 10000
	a.channelScheduler = scheduler.NewChannelScheduler(a.cfgManager, a.messagesMetricsManager, a.responsesMetricsManager, a.geminiMetricsManager, a.chatMetricsManager, a.imagesMetricsManager, session.NewTraceAffinityManager(), warmup.NewURLManager(time.Second, 1), a.vectorsMetricsManager)
	if err := a.cfgManager.AddChatUpstream(config.UpstreamConfig{ChannelUID: "catalog", Name: "catalog", BaseURL: provider.URL, APIKeys: []string{"fake-provider"}, ServiceType: "openai", Status: "active", AllowUnknownContext: true}); err != nil {
		t.Fatal(err)
	}
	handler := httpbridge.Handler(a.newRouter())
	invoke := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", "route-proxy")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	// Populate the cache before governance; this unreviewed catalog must never be reused after activation.
	if w := invoke("GET", "/v1/models", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "provider/OTHER") {
		t.Fatalf("warm models: %d %s", w.Code, w.Body.String())
	}
	view, err := a.cfgManager.GetModelImport("catalog")
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.cfgManager.SetModelImportPolicy("catalog", view.Revision, config.ModelImportPolicy{StripPrefix: "provider/", Case: "lower"}, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	review, err := a.cfgManager.StageModelImport("catalog", []config.ModelImportCandidate{{ID: "provider/GPT-raw"}}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.cfgManager.ApproveModelImport("catalog", review.ID, "reviewer", false, false); err != nil {
		t.Fatal(err)
	}
	offline.Store(true)
	if w := invoke("GET", "/v1/models", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "gpt-raw") || strings.Contains(w.Body.String(), "OTHER") {
		t.Fatalf("stale catalog escaped approval: %d %s", w.Code, w.Body.String())
	}
	body := `{"model":"gpt-raw","messages":[{"role":"user","content":"hello"}]}`
	if w := invoke("POST", "/v1/chat/completions", body); w.Code != 200 {
		t.Fatalf("reviewed alias: %d %s", w.Code, w.Body.String())
	}
	if actual.Load() != "provider/GPT-raw" {
		t.Fatalf("wrong provider target: %v", actual.Load())
	}
	before := calls.Load()
	if w := invoke("POST", "/v1/chat/completions", strings.Replace(body, "gpt-raw", "unreviewed", 1)); w.Code == 200 {
		t.Fatal("unreviewed request succeeded")
	}
	if calls.Load() != before {
		t.Fatal("unreviewed model reached provider")
	}
	if w := invoke("GET", "/v1/models/gpt-raw", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "gpt-raw") {
		t.Fatalf("detail alias: %d %s", w.Code, w.Body.String())
	}
	if actual.Load() != "/v1/models/provider/GPT-raw" {
		t.Fatalf("detail used wrong actual ID: %v", actual.Load())
	}
	before = calls.Load()
	_ = invoke("GET", "/v1/models/unreviewed", "")
	if calls.Load() != before {
		t.Fatal("unreviewed model detail reached provider")
	}
}

func TestModelImportScopedAPIAndStaleReview(t *testing.T) {
	a := newRouteTestApplication(t, true, "production")
	for _, uid := range []string{"owned", "other"} {
		if err := a.cfgManager.AddChatUpstream(config.UpstreamConfig{ChannelUID: uid, Name: uid, BaseURL: "https://private.invalid/" + uid, APIKeys: []string{"provider-secret"}, ServiceType: "openai", Status: "active"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"viewer", "operator", "admin"} {
		if err := a.cfgManager.PutAdminCredential(config.AdminCredential{ID: role, ActorID: "actor-" + role, Role: role, KeyHash: config.EntryKeyHash("key-" + role), ChannelUIDs: []string{"owned"}}, ""); err != nil {
			t.Fatal(err)
		}
	}
	handler := httpbridge.Handler(a.newRouter())
	invoke := func(method, path, key, body, revision string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("x-api-key", key)
		r.Header.Set("If-Match", revision)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	path := "/api/channels/owned/model-import"
	for _, role := range []string{"viewer", "operator", "admin"} {
		w := invoke("GET", path+"?protocol=chat", "key-"+role, "", "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "provider-secret") || strings.Contains(w.Body.String(), "private.invalid") {
			t.Fatalf("unsafe read: %d %s", w.Code, w.Body.String())
		}
		if w = invoke("GET", "/api/channels/other/model-import", "key-"+role, "", ""); w.Code != 403 {
			t.Fatalf("scope bypass: %d", w.Code)
		}
	}
	view, _ := a.cfgManager.GetModelImport("owned")
	for _, role := range []string{"viewer", "operator"} {
		if w := invoke("PUT", path+"/policy", "key-"+role, `{"enabled":false}`, view.Revision); w.Code != 403 {
			t.Fatalf("role mutation bypass: %d", w.Code)
		}
	}
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":false,"approved":[]}`} {
		if w := invoke("PUT", path+"/policy", "key-admin", body, view.Revision); w.Code != 400 {
			t.Fatalf("invalid policy accepted: %d %s", w.Code, w.Body.String())
		}
	}
	if w := invoke("PUT", path+"/policy", "key-admin", `{"enabled":false,"stripPrefix":"provider/"}`, view.Revision); w.Code != 200 {
		t.Fatalf("policy: %d %s", w.Code, w.Body.String())
	}
	if w := invoke("POST", path+"/preview", "key-admin", `{"models":[{"id":"provider/raw"}]}`, view.Revision); w.Code != 409 {
		t.Fatalf("stale preview: %d %s", w.Code, w.Body.String())
	}
	view, _ = a.cfgManager.GetModelImport("owned")
	w := invoke("POST", path+"/preview", "key-admin", `{"models":[{"id":"provider/raw","endpoints":[{"type":"chat","source":"probe","verified":true}]}]}`, view.Revision)
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	var review config.ModelImportReview
	if err := json.Unmarshal(w.Body.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if review.Models[0].Endpoints[0].Verified {
		t.Fatal("operator forged probe evidence")
	}
	// Any source configuration change invalidates an otherwise matching review ID.
	index := 0
	for i, up := range a.cfgManager.GetConfig().ChatUpstream {
		if up.ChannelUID == "owned" {
			index = i
		}
	}
	if _, err := a.cfgManager.UpdateChatUpstream(index, config.UpstreamUpdate{Remark: func() *string { s := "edit"; return &s }()}); err != nil {
		t.Fatal(err)
	}
	if w = invoke("POST", path+"/approve", "key-admin", `{"reviewId":"`+review.ID+`"}`, ""); w.Code != 409 {
		t.Fatalf("stale approval: %d %s", w.Code, w.Body.String())
	}
	if w = invoke("GET", path+"?protocol=responses", "key-admin", "", ""); w.Code != 404 {
		t.Fatalf("wrong protocol accepted: %d", w.Code)
	}
	view, _ = a.cfgManager.GetModelImport("owned")
	if w = invoke("DELETE", path, "key-operator", `{"allowUnrestricted":true}`, view.Revision); w.Code != 403 {
		t.Fatalf("operator reset: %d", w.Code)
	}
	if w = invoke("DELETE", path, "key-admin", `{"allowUnrestricted":false}`, view.Revision); w.Code != 400 {
		t.Fatalf("reset widened empty rules: %d %s", w.Code, w.Body.String())
	}
	if w = invoke("DELETE", path, "key-admin", `{"allowUnrestricted":true}`, view.Revision); w.Code != 200 {
		t.Fatalf("explicit reset: %d %s", w.Code, w.Body.String())
	}
	if a.cfgManager.GetConfig().ChatUpstream[index].ModelImport != nil {
		t.Fatal("reset did not restore legacy lifecycle")
	}
	logical := a.cfgManager.GetConfig().ChatUpstream[index].LogicalChannelUID
	if logical != "" {
		if err := a.cfgManager.PutAdminCredential(config.AdminCredential{ID: "logical", ActorID: "logical-actor", Role: "admin", KeyHash: config.EntryKeyHash("logical-key"), ChannelUIDs: []string{logical}}, ""); err != nil {
			t.Fatal(err)
		}
		if w = invoke("GET", "/api/channels/"+logical+"/model-import?protocol=chat", "logical-key", "", ""); w.Code != 200 {
			t.Fatalf("logical scoped read: %d %s", w.Code, w.Body.String())
		}
	}
}
