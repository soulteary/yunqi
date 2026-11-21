package autopilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/copilot"
)

func TestCopilotDiscoveryUsesModelEndpointsAndPreservesIdentity(t *testing.T) {
	const catalog = `{"data":[{"id":"opaque-a","supported_endpoints":["/responses"]},{"id":"opaque-b","supported_endpoints":["/chat/completions"]},{"id":"opaque-c","supported_endpoints":["/v1/messages"]},{"id":"opaque-d"},{"id":"opaque-e","supported_endpoints":["ws:/responses"]},{"id":"opaque-f","supported_endpoints":["/responses","/chat/completions"]},{"id":"opaque-g","supported_endpoints":[]}]}`
	var mu sync.Mutex
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer runtime-fake" || r.Header.Get("Copilot-Integration-Id") == "" || r.Header.Get("X-Api-Key") != "" {
			t.Errorf("OAuth identity lost: %v", r.Header)
		}
		if r.URL.Path == "/models" {
			mu.Lock()
			calls["models"]++
			mu.Unlock()
			w.Write([]byte(catalog))
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		calls[body.Model+":"+r.URL.Path]++
		mu.Unlock()
		allowed := map[string][]string{"opaque-a": {"/responses"}, "opaque-b": {"/chat/completions"}, "opaque-c": {"/v1/messages"}, "opaque-d": {"/responses"}, "opaque-f": {"/responses", "/chat/completions"}}
		ok := false
		for _, path := range allowed[body.Model] {
			if path == r.URL.Path {
				ok = true
			}
		}
		if !ok {
			t.Errorf("blind endpoint request: %s %s", body.Model, r.URL.Path)
			w.WriteHeader(400)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	resolver := copilot.NewEndpointResolver(func(_ context.Context, key, proxy string) (string, string, error) {
		if key != "github-fake" {
			t.Errorf("wrong credential %q", key)
		}
		return "runtime-fake", server.URL, nil
	}, server.Client())
	runner := NewAutoDiscoveryRunner(nil, nil)
	runner.client = server.Client()
	runner.copilotResolver = resolver
	channel := &config.UpstreamConfig{ServiceType: "copilot", APIKeys: []string{"github-fake"}, BaseURL: "https://unused.invalid", CustomHeaders: map[string]string{"Authorization": "bad", "X-Api-Key": "bad"}, AccountUID: "acct-fake"}
	result := runner.probeEndpoint(context.Background(), server.Client(), channel, channel.BaseURL, "github-fake")
	if !result.ProtocolOk || len(result.ModelDetails) != 7 {
		t.Fatalf("catalog evidence lost: %+v", result)
	}
	runner.discoverEndpointProtocols(context.Background(), channel, channel.BaseURL, "github-fake", &result, nil)
	expected := map[string][]string{"responses": {"opaque-a", "opaque-d", "opaque-f"}, "chat": {"opaque-b", "opaque-f"}, "messages": {"opaque-c"}}
	if !reflect.DeepEqual(result.ProtocolModels, expected) {
		t.Fatalf("ProtocolModels=%v want %v", result.ProtocolModels, expected)
	}
	mu.Lock()
	if calls["models"] != 1 {
		t.Errorf("catalog not reused: %v", calls)
	}
	mu.Unlock()
	for protocol, models := range expected {
		route := discoveredProtocolUpstream(channel, protocol, "fake", true)
		if route.ServiceType != "copilot" || route.CopilotProtocol != protocol || route.APIKeys[0] != "github-fake" {
			t.Fatalf("route lost identity: %+v", route)
		}
		filtered := endpointResultsForProtocol([]EndpointDiscoveryResult{result}, protocol)
		if len(filtered) != 1 || !reflect.DeepEqual(filtered[0].Models, models) {
			t.Fatalf("wrong route evidence: %v", filtered)
		}
	}
	raw, err := json.Marshal(CheckpointedEndpoint{ModelDetails: result.ModelDetails})
	if err != nil {
		t.Fatal(err)
	}
	var restored CheckpointedEndpoint
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.ModelDetails, result.ModelDetails) {
		t.Fatal("checkpoint discarded typed endpoint evidence")
	}
}
