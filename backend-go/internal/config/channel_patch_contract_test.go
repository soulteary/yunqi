package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestChannelPatchOmitNullFalseZeroContract(t *testing.T) {
	no := false
	yes := true
	multiplier := 2.0
	for _, test := range []struct {
		name, body string
		check      func(*testing.T, UpstreamConfig)
	}{
		{"omit", `{"description":"renamed"}`, func(t *testing.T, ch UpstreamConfig) {
			if ch.AutoBlacklistBalance == nil || *ch.AutoBlacklistBalance || ch.CostMultiplier == nil || *ch.CostMultiplier != 2 || ch.RequestTimeoutMs != 1000 || !ch.FastMode {
				t.Fatal("omission changed existing settings")
			}
		}},
		{"null", `{"autoBlacklistBalance":null,"costMultiplier":null,"requestTimeoutMs":null,"fastMode":null,"customHeaders":null,"supportedModels":null}`, func(t *testing.T, ch UpstreamConfig) {
			if ch.AutoBlacklistBalance != nil || !ch.IsAutoBlacklistBalanceEnabled() || ch.CostMultiplier != nil || ch.RequestTimeoutMs != 0 || ch.FastMode || ch.CustomHeaders != nil || ch.SupportedModels != nil {
				t.Fatalf("null did not restore defaults: %+v", ch)
			}
		}},
		{"concrete", `{"autoBlacklistBalance":false,"costMultiplier":0,"requestTimeoutMs":0,"fastMode":false,"customHeaders":{},"supportedModels":[]}`, func(t *testing.T, ch UpstreamConfig) {
			if ch.AutoBlacklistBalance == nil || *ch.AutoBlacklistBalance || ch.CostMultiplier != nil || ch.RequestTimeoutMs != 0 || ch.FastMode || len(ch.CustomHeaders) != 0 || len(ch.SupportedModels) != 0 {
				t.Fatal("false/zero/empty updates lost")
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ch := UpstreamConfig{AutoBlacklistBalance: &no, NormalizeMetadataUserID: &yes, CostMultiplier: &multiplier, RequestTimeoutMs: 1000, FastMode: true, CustomHeaders: map[string]string{"x-test": "old"}, SupportedModels: []string{"old"}}
			var update UpstreamUpdate
			if err := json.Unmarshal([]byte(test.body), &update); err != nil {
				t.Fatal(err)
			}
			if _, err := applyUpstreamUpdateFields(&ch, update); err != nil {
				t.Fatal(err)
			}
			test.check(t, ch)
		})
	}
}

func TestChannelPatchMarshalRetainsExplicitEmptyAndOmitsUnspecified(t *testing.T) {
	update := UpstreamUpdate{Tags: []string{}, CustomHeaders: map[string]string{}, OutboundNetwork: NetworkPatch(nil)}
	data, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 3 || string(raw["tags"]) != "[]" || string(raw["customHeaders"]) != "{}" || string(raw["outboundNetwork"]) != "null" {
		t.Fatalf("unexpected sparse patch: %s", data)
	}
	data, err = json.Marshal(UpstreamUpdate{})
	if err != nil || string(data) != "{}" {
		t.Fatalf("empty patch=%s err=%v", data, err)
	}
}

func TestChannelPatchNullResetSharedSettingsOnly(t *testing.T) {
	var update UpstreamUpdate
	if err := json.Unmarshal([]byte(`{"autoBlacklistBalance":null,"supportedModels":null,"costMultiplier":null}`), &update); err != nil {
		t.Fatal(err)
	}
	shared := sharedChannelUpdate(update)
	if shared == nil {
		t.Fatal("missing shared resets")
	}
	got := map[string]bool{}
	for _, key := range shared.NullFields {
		got[key] = true
	}
	if !reflect.DeepEqual(got, map[string]bool{"autoBlacklistBalance": true, "costMultiplier": true}) {
		t.Fatalf("shared resets=%v", got)
	}
}

func TestChannelPatchRejectsNullIdentity(t *testing.T) {
	for _, field := range []string{"apiKeys", "baseUrl", "serviceType", "status"} {
		var update UpstreamUpdate
		if err := json.Unmarshal([]byte(`{"`+field+`":null}`), &update); err == nil {
			t.Fatalf("null identity field accepted: %s", field)
		}
	}
}

func TestChannelPatchManualAndManagedRoundTrip(t *testing.T) {
	for _, managed := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "managed"}[managed], func(t *testing.T) {
			no := false
			multiplier := 2.5
			path := t.TempDir() + "/config.json"
			cm := &ConfigManager{configFile: path, config: Config{Upstream: []UpstreamConfig{{
				ChannelUID: "ch_roundtrip", AccountUID: "acct_roundtrip", ServiceType: "claude", BaseURL: "https://example.com",
				APIKeys: []string{"sk-local"}, AutoManaged: managed, AutoManagedKind: "generic", AutoBlacklistBalance: &no, CostMultiplier: &multiplier,
				FastMode: true, RequestTimeoutMs: 1234, CustomHeaders: map[string]string{"x-extra": "keep"},
				OutboundNetwork: &OutboundNetworkConfig{ProxyMode: "direct"},
			}}}}
			var update UpstreamUpdate
			if err := json.Unmarshal([]byte(`{"description":"renamed"}`), &update); err != nil {
				t.Fatal(err)
			}
			if _, err := cm.UpdateUpstream(0, update); err != nil {
				t.Fatal(err)
			}
			loaded, err := NewConfigManager(path, "")
			if err != nil {
				t.Fatal(err)
			}
			got := loaded.GetConfig().Upstream[0]
			_ = loaded.Close()
			if !got.FastMode || got.RequestTimeoutMs != 1234 || got.AutoBlacklistBalance == nil || *got.AutoBlacklistBalance || got.CostMultiplier == nil || *got.CostMultiplier != 2.5 || got.CustomHeaders["x-extra"] != "keep" || got.OutboundNetwork == nil || got.OutboundNetwork.ProxyMode != "direct" {
				t.Fatalf("rename lost settings: %+v", got)
			}
			if err := json.Unmarshal([]byte(`{"autoBlacklistBalance":null,"costMultiplier":null,"fastMode":false,"requestTimeoutMs":0,"customHeaders":{}}`), &update); err != nil {
				t.Fatal(err)
			}
			if _, err := cm.UpdateUpstream(0, update); err != nil {
				t.Fatal(err)
			}
			loaded, err = NewConfigManager(path, "")
			if err != nil {
				t.Fatal(err)
			}
			got = loaded.GetConfig().Upstream[0]
			_ = loaded.Close()
			if got.FastMode || got.RequestTimeoutMs != 0 || got.AutoBlacklistBalance != nil || !got.IsAutoBlacklistBalanceEnabled() || got.CostMultiplier != nil || len(got.CustomHeaders) != 0 {
				t.Fatalf("clear did not persist: %+v", got)
			}
		})
	}
}
