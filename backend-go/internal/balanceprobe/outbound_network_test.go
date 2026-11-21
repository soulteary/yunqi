package balanceprobe

import (
	"github.com/soulteary/yunqi/internal/config"
	"testing"
)

func TestUnavailableEgressDoesNotOverwriteQuotaOrKeyHealth(t *testing.T) {
	store := newFakeStore()
	q := &fakeQuota{}
	cfg := config.Config{ChatUpstream: []config.UpstreamConfig{{Name: "bound", ChannelUID: "ch-bound", Status: "active", BaseURL: "http://127.0.0.1:1", APIKeys: []string{"k1", "k2"}, OutboundNetwork: &config.OutboundNetworkConfig{InterfaceName: "yunqi-missing", DNS: config.OutboundDNSConfig{Mode: "bound", Servers: []string{"127.0.0.1:53"}}}}}}
	m := newTestManager(cfg, store, q)
	m.probeChannel("chat", 0)
	if len(store.records) != 0 || len(q.snapshot()) != 0 {
		t.Fatalf("local egress failure polluted key/quota state: records=%v quota=%v", store.records, q.snapshot())
	}
}
