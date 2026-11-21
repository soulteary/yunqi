package autopilot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

func TestHandleEndpointsSeparatesKeyHashAndMask(t *testing.T) {

	apiKey := "sk-real-minimax-token-plan-key"
	channelUID := "ch-minimax"
	profile := newTestProfile("ep-minimax", channelUID, "messages", "https://api.minimax.io")
	profile.KeyHash = KeyHashFromAPIKey(apiKey)
	profile.MetricsKey = profile.KeyHash
	profile.KeyMask = "sk-rea***key"

	store, err := NewProfileStoreWithDB(newTestDB(t))
	if err != nil {
		t.Fatalf("创建 ProfileStore 失败: %v", err)
	}
	if err := store.Upsert(profile); err != nil {
		t.Fatalf("写入 endpoint profile 失败: %v", err)
	}
	mgr := newTestManagerForResolveAPIKey(t, config.Config{
		Upstream: []config.UpstreamConfig{{
			ChannelUID: channelUID,
			BaseURL:    profile.BaseURL,
			APIKeys:    []string{apiKey},
		}},
	})
	mgr.store = store

	router := httpbridge.NewApp()
	router.Get("/health-center/channels/:channelUid/endpoints", handleEndpoints(mgr))
	recorder := httptest.NewRecorder()
	httpbridge.Handler(router).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health-center/channels/"+channelUID+"/endpoints", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response EndpointsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(response.Endpoints) != 1 {
		t.Fatalf("endpoints 数量 = %d, want 1", len(response.Endpoints))
	}
	endpoint := response.Endpoints[0]
	if endpoint.KeyHash != profile.KeyHash {
		t.Errorf("keyHash = %q, want %q", endpoint.KeyHash, profile.KeyHash)
	}
	if endpoint.KeyMask != profile.KeyMask {
		t.Errorf("keyMask = %q, want %q", endpoint.KeyMask, profile.KeyMask)
	}
	if endpoint.KeyHash == apiKey || endpoint.KeyMask == apiKey {
		t.Fatal("响应不得包含明文 API Key")
	}
}
