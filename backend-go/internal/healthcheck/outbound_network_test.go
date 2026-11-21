package healthcheck

import (
	"context"
	"github.com/soulteary/yunqi/internal/config"
	"testing"
)

func TestUnavailableEgressSkipsHealthPenalties(t *testing.T) {
	f := newL2Fixture("chat", config.UpstreamConfig{Name: "bound", Status: "active", BaseURL: "http://127.0.0.1:1", APIKeys: []string{"k1", "k2"}, ServiceType: "openai", OutboundNetwork: &config.OutboundNetworkConfig{InterfaceName: "yunqi-missing", DNS: config.OutboundDNSConfig{Mode: "bound", Servers: []string{"127.0.0.1:53"}}}}, nil)
	calls := 0
	f.manager.RegisterL1Fetcher("chat", func(_ context.Context, _ L1Request) (L1Response, error) { calls++; return L1Response{}, nil })
	f.manager.checkChannel("chat", 0)
	if calls != 0 || f.l1Record(t, "chat") != nil || f.l2Record(t, "chat") != nil {
		t.Fatal("local egress failure polluted health records or sent probe")
	}
}
