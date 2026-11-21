package autopilot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
)

// 拆分处理函数后，六类静态协议路由仍须各自读取对应渠道池，
// 并保持 UID/索引查找、输入校验和发现服务不可用时的响应语义。
func TestAutoManagedRoutesKeepProtocolPoolsIsolated(t *testing.T) {
	protocols := []struct {
		kind, pool, service string
	}{
		{"messages", "upstream", "claude"},
		{"chat", "chatUpstream", "openai"},
		{"responses", "responsesUpstream", "responses"},
		{"gemini", "geminiUpstream", "gemini"},
		{"images", "imagesUpstream", "openai"},
		{"vectors", "vectorsUpstream", "openai"},
	}
	fixture := make(map[string]any)
	for i, protocol := range protocols {
		fixture[protocol.pool] = []map[string]any{{
			"channelUid": "ch_" + protocol.kind,
			"name":       protocol.kind, "serviceType": protocol.service,
			"baseUrl": "https://example.invalid/" + protocol.kind,
			"apiKeys": []string{"test-key-" + protocol.kind}, "status": "active",
			"autoManaged":   i%2 == 0,
			"autoManagedAt": time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC),
		}}
	}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := config.NewConfigManager(path, filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	handler := httpbridge.Handler(setupAutoManagedRouter(&AutoManagedDeps{CfgManager: manager}))
	for i, protocol := range protocols {
		t.Run(protocol.kind, func(t *testing.T) {
			for _, id := range []string{"ch_" + protocol.kind, "0"} {
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/"+protocol.kind+"/channels/"+id+"/auto-status", nil))
				if w.Code != http.StatusOK {
					t.Fatalf("status(%s)=%d: %s", id, w.Code, w.Body.String())
				}
				var response AutoStatusResponse
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				wantTime := time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC)
				if response.AutoManaged != (i%2 == 0) || response.AutoManagedAt == nil || !response.AutoManagedAt.Equal(wantTime) || response.Discovery != nil {
					t.Fatalf("wrong protocol pool for %s: %+v", id, response)
				}
			}
			for _, request := range []struct {
				method, suffix, body string
				status               int
				message              string
			}{
				{http.MethodGet, "ch_missing/auto-status", "", http.StatusNotFound, "渠道不存在"},
				{http.MethodPost, "ch_missing/auto-discover", "", http.StatusNotFound, "渠道不存在"},
				{http.MethodPost, "ch_" + protocol.kind + "/auto-discover", "", http.StatusServiceUnavailable, "发现服务未就绪"},
				{http.MethodPost, "auto-add", `{}`, http.StatusBadRequest, "apiKeys 不能为空"},
			} {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(request.method, "/api/"+protocol.kind+"/channels/"+request.suffix, strings.NewReader(request.body))
				req.Header.Set("Content-Type", "application/json")
				handler.ServeHTTP(w, req)
				if w.Code != request.status {
					t.Fatalf("%s %s: status=%d body=%s", request.method, request.suffix, w.Code, w.Body.String())
				}
				var response map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if got := fmt.Sprint(response["error"]); got != request.message {
					t.Fatalf("%s error=%q, want %q", request.suffix, got, request.message)
				}
			}
		})
	}
}
