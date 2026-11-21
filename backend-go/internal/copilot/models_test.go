package copilot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestModelEndpointSelectionUsesOnlyExplicitHTTPMetadata(t *testing.T) {
	for _, tc := range []struct {
		metadata, preferred, protocol, path string
		reject                              bool
	}{
		{``, "", "responses", "/responses", false},
		{`null`, "", "responses", "/responses", false},
		{`[]`, "", "", "", true},
		{`["ws:/responses"]`, "", "", "", true},
		{`["/v1/messages"]`, "", "messages", "/v1/messages", false},
		{`["/chat/completions","/responses"]`, "", "responses", "/responses", false},
		{`["ws:/responses","/chat/completions"]`, "", "chat", "/chat/completions", false},
		{`["/responses"]`, "chat", "", "", true},
		{`["https://example.invalid/responses","//example.invalid/responses","/responses?x=1"]`, "", "", "", true},
		{`"/responses"`, "", "", "", true},
		{`["/responses",1]`, "", "", "", true},
		{`["/responses"]`, "invalid", "", "", true},
	} {
		t.Run(fmt.Sprintf("%s/%s", tc.metadata, tc.preferred), func(t *testing.T) {
			model := `{"id":"claude-unknown-gpt"}`
			if tc.metadata != "" {
				model = `{"id":"claude-unknown-gpt","supported_endpoints":` + tc.metadata + `}`
			}
			details := ParseModelDetails([]byte(`{"data":[` + model + `]}`))
			protocol, path, err := SelectEndpoint(details, "claude-unknown-gpt", tc.preferred)
			if tc.reject {
				if err == nil {
					t.Fatal("accepted unsupported metadata")
				}
				return
			}
			if err != nil || protocol != tc.protocol || path != tc.path {
				t.Fatalf("got %s %s %v", protocol, path, err)
			}
		})
	}
}

func TestEndpointResolverCacheCredentialIsolationAndCopies(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/models" || r.Header.Get("Copilot-Integration-Id") == "" {
			t.Errorf("request=%s headers=%v", r.URL.Path, r.Header)
		}
		if r.Header.Get("Authorization") == "Bearer first" {
			fmt.Fprint(w, `{"data":[{"id":"opaque","supported_endpoints":["/responses"]}]}`)
		} else {
			fmt.Fprint(w, `{"data":[{"id":"opaque","supported_endpoints":["/chat/completions"]}]}`)
		}
	}))
	defer server.Close()
	resolver := NewEndpointResolver(func(_ context.Context, key, proxy string) (string, string, error) { return key, server.URL, nil }, server.Client())
	_, _, models, err := resolver.Models(context.Background(), "first", "")
	if err != nil {
		t.Fatal(err)
	}
	models[0].SupportedEndpoints[0] = "/v1/messages"
	for _, tc := range []struct{ key, protocol string }{{"first", "responses"}, {"second", "chat"}, {"first", "responses"}} {
		_, _, protocol, _, err := resolver.Resolve(context.Background(), tc.key, "", "opaque", "")
		if err != nil || protocol != tc.protocol {
			t.Fatalf("key=%s protocol=%s err=%v", tc.key, protocol, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("catalog calls=%d", calls.Load())
	}
	resolver.mu.Lock()
	for i := range 256 {
		resolver.cache[[32]byte{byte(i), 1}] = modelCacheEntry{}
	}
	resolver.mu.Unlock()
	if _, _, _, err := resolver.Models(context.Background(), "third", ""); err != nil {
		t.Fatal(err)
	}
	if len(resolver.cache) > 256 {
		t.Fatal("unbounded model catalog cache")
	}

}

func TestEndpointResolverDoesNotInferAfterCatalogFailure(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		legacy bool
	}{
		{401, `{}`, false}, {403, `{}`, false}, {429, `{}`, false}, {500, `{}`, false}, {200, `not JSON`, false}, {404, `{}`, true}, {410, `{}`, true},
	} {
		t.Run(fmt.Sprint(tc.status, tc.body), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			resolver := NewEndpointResolver(func(context.Context, string, string) (string, string, error) { return "runtime", server.URL, nil }, server.Client())
			_, _, protocol, endpoint, err := resolver.Resolve(context.Background(), "github-fake", "", "opaque", "")
			if tc.legacy {
				if err != nil || protocol != "responses" || endpoint != "/responses" {
					t.Fatalf("legacy path: %s %s %v", protocol, endpoint, err)
				}
			} else if err == nil {
				t.Fatal("catalog failure allowed inference")
			}
		})
	}
}
