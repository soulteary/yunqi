package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOutboundNetworkValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		c     OutboundNetworkConfig
		proxy string
		valid bool
	}{
		{"direct preserves unused invalid proxy", OutboundNetworkConfig{ProxyMode: "direct"}, "unused", true},
		{"proxy needs URL", OutboundNetworkConfig{ProxyMode: "proxy"}, "", false},
		{"unsupported proxy", OutboundNetworkConfig{ProxyMode: "proxy"}, "ftp://localhost", false},
		{"valid proxy", OutboundNetworkConfig{ProxyMode: "proxy"}, "socks5://localhost:1080", true},
		{"invalid family", OutboundNetworkConfig{AddressFamily: "v5"}, "", false},
		{"missing bound DNS", OutboundNetworkConfig{InterfaceName: "en0"}, "", false},
		{"DNS hostname forbidden", OutboundNetworkConfig{DNS: OutboundDNSConfig{Mode: "bound", Servers: []string{"dns.example:53"}}}, "", false},
		{"wrong zone", OutboundNetworkConfig{InterfaceName: "en0", DNS: OutboundDNSConfig{Mode: "bound", Servers: []string{"[fe80::1%en1]:53"}}}, "", false},
		{"valid absent interface can save", OutboundNetworkConfig{InterfaceName: "absent0", DNS: OutboundDNSConfig{Mode: "bound", Servers: []string{"192.0.2.53:53"}}}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateOutboundNetwork(&tc.c, tc.proxy)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
		})
	}
}
func TestOutboundNetworkPatchAndLogicalProjection(t *testing.T) {
	initial := &OutboundNetworkConfig{ProxyMode: "direct", DNS: OutboundDNSConfig{Mode: "bound", Servers: []string{"127.0.0.1:53"}}}
	for _, tc := range []struct {
		raw  string
		want *OutboundNetworkConfig
	}{{`{}`, initial}, {`{"outboundNetwork":null}`, nil}, {`{"outboundNetwork":{"proxyMode":"legacy"}}`, &OutboundNetworkConfig{ProxyMode: "legacy"}}} {
		up := UpstreamConfig{OutboundNetwork: CloneOutboundNetwork(initial)}
		var patch UpstreamUpdate
		if err := json.Unmarshal([]byte(tc.raw), &patch); err != nil {
			t.Fatal(err)
		}
		shared := sharedChannelUpdate(patch)
		if shared != nil {
			if _, err := applyUpstreamUpdateFields(&up, *shared); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(up.OutboundNetwork, tc.want) {
			t.Fatalf("%s => %+v", tc.raw, up.OutboundNetwork)
		}
		settings := logicalSettingsFromRoute(up)
		sibling := UpstreamConfig{OutboundNetwork: initial}
		if _, err := applyUpstreamUpdateFields(&sibling, logicalSettingsUpdate(settings)); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(sibling.OutboundNetwork, tc.want) {
			t.Fatal("logical projection lost clear/set")
		}
		if sibling.OutboundNetwork != nil {
			sibling.OutboundNetwork.ProxyMode = "direct_first"
			if up.OutboundNetwork.ProxyMode == "direct_first" {
				t.Fatal("shared mutable policy")
			}
		}
	}
}

func TestOutboundPolicyPersistsAcrossSixSiblingsAndReload(t *testing.T) {
	cm := newLogicalTestCM(t, func(c *Config) {})
	kinds := []string{"messages", "chat", "responses", "gemini", "images", "vectors"}
	protocols := []CreateLogicalChannelProtocol{}
	for _, kind := range kinds {
		protocols = append(protocols, CreateLogicalChannelProtocol{Kind: kind, APIKeys: []string{"key"}})
	}
	lc, err := cm.CreateLogicalChannel(CreateLogicalChannelInput{Name: "network-shared", BaseURLs: []string{"https://example.com"}, Protocols: protocols})
	if err != nil {
		t.Fatal(err)
	}
	cfg := cm.GetConfig()
	first := cfg.Upstream[0]
	loc, _, ok := cm.FindUpstreamByUID(first.ChannelUID)
	if !ok {
		t.Fatal("missing route")
	}
	policy := &OutboundNetworkConfig{ProxyMode: "direct"}
	if _, err = UpdateUpstreamByKind(cm, loc, UpstreamUpdate{OutboundNetwork: NetworkPatch(policy)}); err != nil {
		t.Fatal(err)
	}
	cfg = cm.GetConfig()
	for _, kind := range kinds {
		routes := *ChannelKindRegistry[ChannelKind(kind)].SliceRef(&cfg)
		if len(routes) != 1 || !reflect.DeepEqual(routes[0].OutboundNetwork, policy) {
			t.Fatalf("%s lost shared policy: %+v", kind, routes)
		}
	}
	if got := cm.GetLogicalChannel(lc.LogicalChannelUID); got.Settings == nil || !reflect.DeepEqual(got.Settings.OutboundNetwork, policy) {
		t.Fatal("logical settings lost policy")
	}
	reloadFile := filepath.Join(t.TempDir(), "config.json")
	persisted, readErr := os.ReadFile(cm.configFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err := os.WriteFile(reloadFile, persisted, 0600); err != nil {
		t.Fatal(err)
	}
	reloaded, loadErr := NewConfigManager(reloadFile, "")
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	defer reloaded.Close()
	diskCfg := reloaded.GetConfig()
	for _, kind := range kinds {
		routes := *ChannelKindRegistry[ChannelKind(kind)].SliceRef(&diskCfg)
		if len(routes) != 1 || !reflect.DeepEqual(routes[0].OutboundNetwork, policy) {
			t.Fatalf("%s lost policy after V3 reload", kind)
		}
	}
	snapshot := cm.GetConfig()
	snapshot.Upstream[0].OutboundNetwork.ProxyMode = "proxy"
	snapshot.LogicalChannels[0].Settings.OutboundNetwork.ProxyMode = "proxy"
	if cm.GetConfig().Upstream[0].OutboundNetwork.ProxyMode != "direct" || cm.GetConfig().LogicalChannels[0].Settings.OutboundNetwork.ProxyMode != "direct" {
		t.Fatal("config snapshot shares mutable policy")
	}
	if _, err = cm.UpdateLogicalChannel(UpdateLogicalChannelInput{LogicalChannelUID: lc.LogicalChannelUID, Protocols: []UpdateLogicalChannelProtocol{{Kind: "chat", ProxyURL: "http://127.0.0.1:7890", OutboundNetwork: NetworkPatch(&OutboundNetworkConfig{ProxyMode: "proxy"})}}}); err != nil {
		t.Fatal(err)
	}
	cfg = cm.GetConfig()
	for _, kind := range kinds {
		routes := *ChannelKindRegistry[ChannelKind(kind)].SliceRef(&cfg)
		if routes[0].ProxyURL != "http://127.0.0.1:7890" || routes[0].OutboundNetwork.ProxyMode != "proxy" {
			t.Fatalf("%s proxy mode and URL diverged", kind)
		}
	}
	if _, err = cm.UpdateLogicalChannel(UpdateLogicalChannelInput{LogicalChannelUID: lc.LogicalChannelUID, Protocols: []UpdateLogicalChannelProtocol{{Kind: "chat", OutboundNetwork: NetworkPatch(nil)}}}); err != nil {
		t.Fatal(err)
	}
	cfg = cm.GetConfig()
	for _, kind := range kinds {
		routes := *ChannelKindRegistry[ChannelKind(kind)].SliceRef(&cfg)
		if routes[0].OutboundNetwork != nil {
			t.Fatalf("%s lost explicit clear", kind)
		}
	}
}
