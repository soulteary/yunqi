package common

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/metrics"
	"github.com/soulteary/yunqi/internal/scheduler"
	"github.com/soulteary/yunqi/internal/types"
	"github.com/soulteary/yunqi/internal/warmup"
)

func TestModelImportAllowsApprovedFederatedExecution(t *testing.T) {

	type upstreamRequest struct {
		Model            string   `json:"model"`
		Temperature      *float64 `json:"temperature"`
		TopP             *float64 `json:"top_p"`
		PresencePenalty  *float64 `json:"presence_penalty"`
		FrequencyPenalty *float64 `json:"frequency_penalty"`
	}
	received := make(chan upstreamRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request upstreamRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		received <- request
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfgManager, channelScheduler, messagesMetrics, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{
		Name:        "k3-chat",
		ChannelUID:  "ch_k3_chat",
		BaseURL:     server.URL,
		APIKeys:     []string{"sk-k3"},
		Status:      "active",
		ServiceType: "openai",
		ModelImport: &config.ModelImportState{Policy: config.ModelImportPolicy{Enabled: true}, Approved: []config.ImportedModel{{ID: "public", UpstreamID: "kimi-k3"}}},
	})
	defer cleanup()

	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-5"}`)))

	requestBody := []byte(`{"model":"claude-sonnet-5","temperature":0.7,"top_p":0.8,"presence_penalty":0.5,"frequency_penalty":0.5,"messages":[]}`)
	executionRoute := scheduler.ChannelRouteRef{Kind: string(scheduler.ChannelKindChat), Index: 0, ChannelUID: "ch_k3_chat"}

	cfg := cfgManager.GetConfig()
	upstream := &cfg.Upstream[0]
	handled, successKey, _, failoverErr, _, lastErr := TryUpstreamWithAllKeys(
		c,
		config.NewEnvConfig(),
		cfgManager,
		channelScheduler,
		scheduler.ChannelKindMessages,
		"Messages",
		messagesMetrics,
		upstream,
		[]warmup.URLLatencyResult{{URL: server.URL, OriginalIdx: 0}},
		requestBody,
		nil,
		false,
		func(upstream *config.UpstreamConfig, failedKeys map[string]bool) (string, error) {
			return cfgManager.GetNextAPIKey(upstream, failedKeys, ChannelAPIType(scheduler.ChannelKindChat))
		},
		func(c fiber.Ctx, upstreamCopy *config.UpstreamConfig, apiKey string) (*http.Request, error) {
			body, _ := httpbridge.Value(c, "requestBodyBytes")
			raw, _ := body.([]byte)
			return http.NewRequestWithContext(httpbridge.Request(c).Context(), http.MethodPost, upstreamCopy.BaseURL, strings.NewReader(string(raw)))
		},
		func(apiKey string) {},
		nil,
		nil,
		func(c fiber.Ctx, resp *http.Response, upstreamCopy *config.UpstreamConfig, apiKey string, actualRequestBody []byte) (*types.Usage, error) {
			_ = resp.Body.Close()
			return nil, nil
		},
		"claude-sonnet-5",
		"",
		0,
		channelScheduler.GetChannelLogStoreForRoute(executionRoute),
		WithExecutionRoute(executionRoute),
		WithExecutionModel("kimi-k3"),
	)

	if !handled || failoverErr != nil || lastErr != nil {
		t.Fatalf("federated attempt failed: handled=%v failoverErr=%#v lastErr=%v", handled, failoverErr, lastErr)
	}
	if successKey != "sk-k3" {
		t.Fatalf("successKey = %q, want sk-k3", successKey)
	}

	select {
	case request := <-received:
		if request.Model != "kimi-k3" {
			t.Fatalf("upstream model = %q, want kimi-k3", request.Model)
		}
		if request.Temperature != nil || request.TopP != nil || request.PresencePenalty != nil || request.FrequencyPenalty != nil {
			t.Fatalf("K3 fixed-value params were not stripped: %#v", request)
		}
	default:
		t.Fatal("upstream never received the federated request")
	}

	// 指标、日志与 key 归属必须落在执行协议（chat），不是请求协议（messages）。
	chatServiceType := scheduler.NormalizedMetricsServiceType(scheduler.ChannelKindChat, upstream.ServiceType)
	identity := metrics.GenerateMetricsIdentityKey(server.URL, "sk-k3", chatServiceType)
	logs := channelScheduler.GetChannelLogStoreForRoute(executionRoute).Get(identity)
	if len(logs) != 1 {
		t.Fatalf("chat-route channel logs = %d, want 1", len(logs))
	}
	if logs[0].BaseURL != server.URL || logs[0].KeyMask == "" {
		t.Fatalf("log attribution wrong: %#v", logs[0])
	}
	if logs[0].InterfaceType != "" && !strings.EqualFold(logs[0].InterfaceType, "chat") {
		t.Fatalf("interfaceType = %q, want chat execution protocol", logs[0].InterfaceType)
	}
	if messagesLogs := channelScheduler.GetChannelLogStore(scheduler.ChannelKindMessages).Get(identity); len(messagesLogs) != 0 {
		t.Fatalf("messages store must not receive sibling execution logs: %#v", messagesLogs)
	}
}

func TestModelImportExactAliasOverridesUnreviewedExecution(t *testing.T) {

	type upstreamRequest struct {
		Model            string   `json:"model"`
		Temperature      *float64 `json:"temperature"`
		TopP             *float64 `json:"top_p"`
		PresencePenalty  *float64 `json:"presence_penalty"`
		FrequencyPenalty *float64 `json:"frequency_penalty"`
	}
	received := make(chan upstreamRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request upstreamRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		received <- request
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfgManager, channelScheduler, messagesMetrics, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{
		Name:        "k3-chat",
		ChannelUID:  "ch_k3_chat",
		BaseURL:     server.URL,
		APIKeys:     []string{"sk-k3"},
		Status:      "active",
		ServiceType: "openai",
		ModelImport: &config.ModelImportState{Policy: config.ModelImportPolicy{Enabled: true}, Approved: []config.ImportedModel{{ID: "public", UpstreamID: "kimi-k3"}}},
	})
	defer cleanup()

	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"public"}`)))

	requestBody := []byte(`{"model":"public","temperature":0.7,"top_p":0.8,"presence_penalty":0.5,"frequency_penalty":0.5,"messages":[]}`)
	executionRoute := scheduler.ChannelRouteRef{Kind: string(scheduler.ChannelKindChat), Index: 0, ChannelUID: "ch_k3_chat"}

	cfg := cfgManager.GetConfig()
	upstream := &cfg.Upstream[0]
	handled, successKey, _, failoverErr, _, lastErr := TryUpstreamWithAllKeys(
		c,
		config.NewEnvConfig(),
		cfgManager,
		channelScheduler,
		scheduler.ChannelKindMessages,
		"Messages",
		messagesMetrics,
		upstream,
		[]warmup.URLLatencyResult{{URL: server.URL, OriginalIdx: 0}},
		requestBody,
		nil,
		false,
		func(upstream *config.UpstreamConfig, failedKeys map[string]bool) (string, error) {
			return cfgManager.GetNextAPIKey(upstream, failedKeys, ChannelAPIType(scheduler.ChannelKindChat))
		},
		func(c fiber.Ctx, upstreamCopy *config.UpstreamConfig, apiKey string) (*http.Request, error) {
			body, _ := httpbridge.Value(c, "requestBodyBytes")
			raw, _ := body.([]byte)
			return http.NewRequestWithContext(httpbridge.Request(c).Context(), http.MethodPost, upstreamCopy.BaseURL, strings.NewReader(string(raw)))
		},
		func(apiKey string) {},
		nil,
		nil,
		func(c fiber.Ctx, resp *http.Response, upstreamCopy *config.UpstreamConfig, apiKey string, actualRequestBody []byte) (*types.Usage, error) {
			_ = resp.Body.Close()
			return nil, nil
		},
		"public",
		"",
		0,
		channelScheduler.GetChannelLogStoreForRoute(executionRoute),
		WithExecutionRoute(executionRoute),
		WithExecutionModel("unreviewed-family-target"),
	)

	if !handled || failoverErr != nil || lastErr != nil {
		t.Fatalf("federated attempt failed: handled=%v failoverErr=%#v lastErr=%v", handled, failoverErr, lastErr)
	}
	if successKey != "sk-k3" {
		t.Fatalf("successKey = %q, want sk-k3", successKey)
	}

	select {
	case request := <-received:
		if request.Model != "kimi-k3" {
			t.Fatalf("upstream model = %q, want kimi-k3", request.Model)
		}
		if request.Temperature != nil || request.TopP != nil || request.PresencePenalty != nil || request.FrequencyPenalty != nil {
			t.Fatalf("K3 fixed-value params were not stripped: %#v", request)
		}
	default:
		t.Fatal("upstream never received the federated request")
	}

	// 指标、日志与 key 归属必须落在执行协议（chat），不是请求协议（messages）。
	chatServiceType := scheduler.NormalizedMetricsServiceType(scheduler.ChannelKindChat, upstream.ServiceType)
	identity := metrics.GenerateMetricsIdentityKey(server.URL, "sk-k3", chatServiceType)
	logs := channelScheduler.GetChannelLogStoreForRoute(executionRoute).Get(identity)
	if len(logs) != 1 {
		t.Fatalf("chat-route channel logs = %d, want 1", len(logs))
	}
	if logs[0].BaseURL != server.URL || logs[0].KeyMask == "" {
		t.Fatalf("log attribution wrong: %#v", logs[0])
	}
	if logs[0].InterfaceType != "" && !strings.EqualFold(logs[0].InterfaceType, "chat") {
		t.Fatalf("interfaceType = %q, want chat execution protocol", logs[0].InterfaceType)
	}
	if messagesLogs := channelScheduler.GetChannelLogStore(scheduler.ChannelKindMessages).Get(identity); len(messagesLogs) != 0 {
		t.Fatalf("messages store must not receive sibling execution logs: %#v", messagesLogs)
	}
}

func TestModelImportRejectsUnapprovedFinalExecution(t *testing.T) {

	type upstreamRequest struct {
		Model            string   `json:"model"`
		Temperature      *float64 `json:"temperature"`
		TopP             *float64 `json:"top_p"`
		PresencePenalty  *float64 `json:"presence_penalty"`
		FrequencyPenalty *float64 `json:"frequency_penalty"`
	}
	received := make(chan upstreamRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request upstreamRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		received <- request
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfgManager, channelScheduler, messagesMetrics, cleanup := newTestFailoverDependencies(t, config.UpstreamConfig{
		Name:        "k3-chat",
		ChannelUID:  "ch_k3_chat",
		BaseURL:     server.URL,
		APIKeys:     []string{"sk-k3"},
		Status:      "active",
		ServiceType: "openai",
		ModelImport: &config.ModelImportState{Policy: config.ModelImportPolicy{Enabled: true}, Approved: []config.ImportedModel{{ID: "public", UpstreamID: "kimi-k3"}}},
	})
	defer cleanup()

	w := httptest.NewRecorder()
	c, _ := httpbridge.TestContext(w)
	httpbridge.SetRequest(c, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-5"}`)))

	requestBody := []byte(`{"model":"claude-sonnet-5","temperature":0.7,"top_p":0.8,"presence_penalty":0.5,"frequency_penalty":0.5,"messages":[]}`)
	executionRoute := scheduler.ChannelRouteRef{Kind: string(scheduler.ChannelKindChat), Index: 0, ChannelUID: "ch_k3_chat"}

	cfg := cfgManager.GetConfig()
	upstream := &cfg.Upstream[0]
	handled, _, _, _, _, lastErr := TryUpstreamWithAllKeys(
		c,
		config.NewEnvConfig(),
		cfgManager,
		channelScheduler,
		scheduler.ChannelKindMessages,
		"Messages",
		messagesMetrics,
		upstream,
		[]warmup.URLLatencyResult{{URL: server.URL, OriginalIdx: 0}},
		requestBody,
		nil,
		false,
		func(upstream *config.UpstreamConfig, failedKeys map[string]bool) (string, error) {
			return cfgManager.GetNextAPIKey(upstream, failedKeys, ChannelAPIType(scheduler.ChannelKindChat))
		},
		func(c fiber.Ctx, upstreamCopy *config.UpstreamConfig, apiKey string) (*http.Request, error) {
			body, _ := httpbridge.Value(c, "requestBodyBytes")
			raw, _ := body.([]byte)
			return http.NewRequestWithContext(httpbridge.Request(c).Context(), http.MethodPost, upstreamCopy.BaseURL, strings.NewReader(string(raw)))
		},
		func(apiKey string) {},
		nil,
		nil,
		func(c fiber.Ctx, resp *http.Response, upstreamCopy *config.UpstreamConfig, apiKey string, actualRequestBody []byte) (*types.Usage, error) {
			_ = resp.Body.Close()
			return nil, nil
		},
		"claude-sonnet-5",
		"",
		0,
		channelScheduler.GetChannelLogStoreForRoute(executionRoute),
		WithExecutionRoute(executionRoute),
		WithExecutionModel("unreviewed"),
	)

	if handled || lastErr == nil {
		t.Fatalf("unreviewed final model was accepted: handled=%v err=%v", handled, lastErr)
	}
	select {
	case <-received:
		t.Fatal("unapproved actual model reached provider")
	default:
	}
}

func TestModelImportPreservesExplicitOverflowExecution(t *testing.T) {
	for _, approved := range []bool{true, false} {
		t.Run(fmt.Sprint(approved), func(t *testing.T) {
			var calls atomic.Int64
			var sent string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body struct {
					Model string `json:"model"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				sent = body.Model
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer server.Close()
			h := newCrossModelHarness(t, server.URL)
			defer h.cleanup()
			models := []config.ImportedModel{{ID: "gpt-5.6-sol", UpstreamID: "provider/small"}}
			if approved {
				models = append(models, config.ImportedModel{ID: "large", UpstreamID: "gpt-5.6-openai"})
			}
			h.upstream.ModelImport = &config.ModelImportState{Policy: config.ModelImportPolicy{Enabled: true}, Approved: models}
			handled, _, err, _ := h.run(t, []byte(`{"model":"gpt-5.6-sol","input":"hello"}`), WithSelectionTrace(&scheduler.SelectionResult{OverflowRedirect: true}))
			if approved {
				if !handled || err != nil || sent != "gpt-5.6-openai" {
					t.Fatalf("approved overflow overwritten by original alias: handled=%v model=%s err=%v", handled, sent, err)
				}
			} else if handled || calls.Load() != 0 || err == nil {
				t.Fatalf("unapproved overflow reached supplier: handled=%v calls=%d err=%v", handled, calls.Load(), err)
			}
		})
	}
}
