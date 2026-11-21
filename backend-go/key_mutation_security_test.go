package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestChannelKeyMutationsKeepKeysOutOfURL(t *testing.T) {
	for _, kind := range []string{"messages", "chat", "responses", "gemini", "images", "vectors"} {
		t.Run(kind, func(t *testing.T) {
			a := newRouteTestApplication(t, true, "production")
			k, err := config.ChannelKindByName(kind)
			if err != nil {
				t.Fatal(err)
			}
			const key = "DUMMY_REVIEW_KEY/with space+percent%"
			if err := config.AddUpstreamByKind(a.cfgManager, k, config.UpstreamConfig{Name: "test", BaseURL: "https://example.invalid", APIKeys: []string{"DUMMY_FIRST", key, "DUMMY_LAST"}}, ""); err != nil {
				t.Fatal(err)
			}
			h := httpbridge.Handler(a.newRouter())
			send := func(method, path, body, auth string, want int) {
				t.Helper()
				req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
				req.Header.Set("Content-Type", "application/json")
				if auth != "" {
					req.Header.Set("x-api-key", auth)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if w.Code != want {
					t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
				}
			}
			check := func(want []string) {
				t.Helper()
				got := k.Slice(a.cfgManager.GetConfig())[0].APIKeys
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("keys = %q, want %q", got, want)
				}
			}
			body, _ := json.Marshal(map[string]string{"apiKey": key})
			base := "/api/" + kind + "/channels/0/keys/"
			send(http.MethodPost, base+"remove", string(body), "", 401)
			check([]string{"DUMMY_FIRST", key, "DUMMY_LAST"})
			for _, bad := range []string{`{}`, `{"apiKey":""}`, `{"apiKey":123}`, `{`} {
				send(http.MethodPost, base+"remove", bad, "route-admin", 400)
			}
			send(http.MethodPost, base+"move-to-top", string(body), "route-admin", 200)
			check([]string{key, "DUMMY_FIRST", "DUMMY_LAST"})
			send(http.MethodPost, base+"move-to-bottom", string(body), "route-admin", 200)
			check([]string{"DUMMY_FIRST", "DUMMY_LAST", key})
			// Older clients keep working while the console sends JSON-only keys.
			send(http.MethodPost, base+url.PathEscape("DUMMY_LAST")+"/top", "", "route-admin", 200)
			check([]string{"DUMMY_LAST", "DUMMY_FIRST", key})
			send(http.MethodPost, base+"remove", string(body), "route-admin", 200)
			check([]string{"DUMMY_LAST", "DUMMY_FIRST"})
		})
	}
}
