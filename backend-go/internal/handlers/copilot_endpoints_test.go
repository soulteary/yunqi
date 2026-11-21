package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/copilot"
)

func TestCopilotDiscoveryAndCapabilityShareEndpointEvidence(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer runtime-fake" || r.Header.Get("Copilot-Integration-Id") == "" || r.Header.Get("X-Api-Key") != "" {
			t.Errorf("identity headers=%v", r.Header)
		}
		if r.URL.Path == "/models" {
			w.Write([]byte(`{"data":[{"id":"opaque","supported_endpoints":["/chat/completions"]}]}`))
			return
		}
		posts.Add(1)
		if r.URL.Path != "/chat/completions" {
			t.Errorf("blind endpoint %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["messages"] == nil {
			t.Errorf("wrong wire body %v", body)
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	old := capabilityCopilotResolver
	capabilityCopilotResolver = copilot.NewEndpointResolver(func(_ context.Context, key, proxy string) (string, string, error) {
		if key != "github-fake" {
			t.Errorf("credential=%q", key)
		}
		return "runtime-fake", server.URL, nil
	}, server.Client())
	defer func() { capabilityCopilotResolver = old }()
	channel := &config.UpstreamConfig{ServiceType: "copilot", BaseURL: "https://unused.invalid", APIKeys: []string{"github-fake"}, CustomHeaders: map[string]string{"Authorization": "bad", "X-Api-Key": "bad"}}
	discovered := discoverTransientModels(context.Background(), channel, "responses", "github-fake")
	if len(discovered.ModelDetails) != 1 || len(discovered.Items) != 1 {
		t.Fatalf("metadata not retained: %+v", discovered)
	}
	models, err := fetchChannelRecognizedModels(context.Background(), channel, "responses", nil, channel.BaseURL, "github-fake")
	if err != nil || strings.Join(models, ",") != "opaque" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	for _, protocol := range []string{"chat", "responses->chat", "messages->chat"} {
		req, err := buildTestRequestWithModel(protocol, channel, "opaque")
		if err != nil {
			t.Fatalf("%s: %v", protocol, err)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	for _, protocol := range []string{"responses", "messages", "gemini", "chat->responses"} {
		if _, err := buildTestRequestWithModel(protocol, channel, "opaque"); err == nil {
			t.Fatalf("accepted unsupported protocol %s", protocol)
		}
	}
	req, err := buildOpenAIChatCompatRequest(channel.BaseURL, []byte(`{"model":"opaque","messages":[{"role":"user","content":"hello"}],"stream":true}`), channel, "github-fake")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := buildResponsesCompatRequest(channel.BaseURL, []byte(`{"model":"opaque","input":"hello"}`), channel, "github-fake"); err == nil {
		t.Fatal("compat probe ignored endpoint evidence")
	}
	if posts.Load() != 4 {
		t.Fatalf("unexpected inference attempts %d", posts.Load())
	}
}
