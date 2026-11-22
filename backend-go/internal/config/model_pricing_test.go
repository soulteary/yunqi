package config

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPricingSnapshotVersionIsContentBasedAndIndependent(t *testing.T) {
	price := 0.0
	p := &ModelPricing{VersionID: "caller-controlled", CacheCreation5mPrice: &price, ImagePrices: []ImagePriceRule{{Quality: "high", Price: &price}}}
	snapshot := SnapshotModelPricing(p)
	if snapshot.VersionID == "" || snapshot.VersionID == p.VersionID || snapshot.VersionID != PricingVersionID(p) {
		t.Fatal("snapshot version is not content based")
	}
	original := snapshot.VersionID
	price = 3
	if *snapshot.CacheCreation5mPrice != 0 || *snapshot.ImagePrices[0].Price != 0 || PricingVersionID(p) == original {
		t.Fatal("snapshot aliases a mutable price")
	}
	if ValidateModelPricing(snapshot) != nil {
		t.Fatal("explicit zero rejected")
	}
	snapshot.CacheCreation5mPrice = &price
	price = math.NaN()
	if ValidateModelPricing(snapshot) == nil {
		t.Fatal("non-finite price accepted")
	}
}
func TestPricingRuleEvaluationBudget(t *testing.T) {
	p := &ModelPricing{ImagePrices: make([]ImagePriceRule, 65)}
	if ValidateModelPricing(p) == nil {
		t.Fatal("unbounded image rules accepted")
	}
	p = &ModelPricing{Tiers: make([]ModelPricingTier, 129)}
	if ValidateModelPricing(p) == nil {
		t.Fatal("unbounded tiers accepted")
	}
}

func TestTypedPricingRevisionAndInvalidSaveRemainConsistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"chatUpstream":[{"channelUid":"ch_price","baseUrl":"https://example.test","serviceType":"openai","apiKeys":["test-key"],"modelCapabilities":{"m":{"pricing":{"inputCacheMissPrice":1,"cacheCreation5mPrice":0}}}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cm, err := NewConfigManager(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	uid := cm.GetConfig().ChatUpstream[0].ChannelUID
	_, _, revisions := cm.ChannelSaveViews()
	description := "changed"
	receipt, _, _, err := cm.UpdateChannelWithReceipt(uid, revisions[uid], UpstreamUpdate{Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	_, _, afterRevisions := cm.ChannelSaveViews()
	if afterRevisions[uid] != receipt.Revision {
		t.Fatal("save receipt and read snapshot revisions diverged")
	}
	before, _ := os.ReadFile(path)
	bad := -1.0
	if _, _, _, err := cm.UpdateChannelWithReceipt(uid, receipt.Revision, UpstreamUpdate{ModelCapabilities: map[string]UpstreamModelCapability{"m": {Pricing: &ModelPricing{CacheCreation5mPrice: &bad}}}}); err == nil {
		t.Fatal("invalid typed price accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid pricing replaced durable configuration")
	}
	if *cm.GetConfig().ChatUpstream[0].ModelCapabilities["m"].Pricing.CacheCreation5mPrice != 0 {
		t.Fatal("invalid save changed live price")
	}
}

func TestAddTypedPricingValidationAndPersistenceFailuresRollbackAllKinds(t *testing.T) {
	adds := map[string]func(*ConfigManager, UpstreamConfig, ...string) error{"messages": (*ConfigManager).AddUpstream, "chat": (*ConfigManager).AddChatUpstream, "responses": (*ConfigManager).AddResponsesUpstream, "gemini": (*ConfigManager).AddGeminiUpstream, "images": (*ConfigManager).AddImagesUpstream, "vectors": (*ConfigManager).AddVectorsUpstream}
	for name, add := range adds {
		for _, failure := range []string{"invalid_price", "persistence"} {
			t.Run(name+"/"+failure, func(t *testing.T) {
				kind, _ := ChannelKindByName(name)
				cfg := Config{}
				*kind.SliceRef(&cfg) = []UpstreamConfig{{ChannelUID: "existing", BaseURL: "https://example.test", ServiceType: kind.DefaultServiceType, APIKeys: []string{"fake"}, Priority: 1}}
				path := filepath.Join(t.TempDir(), "config.json")
				data, _ := json.Marshal(cfg)
				if failure == "persistence" {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
					pathSentinel := filepath.Join(path, "existing")
					if err := os.WriteFile(pathSentinel, data, 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				cm := &ConfigManager{config: cfg, configFile: path}
				before := cm.config.deepCopy()
				price := 1.0
				if failure == "invalid_price" {
					price = -1
				}
				err := add(cm, UpstreamConfig{BaseURL: "https://new.example.test", ServiceType: kind.DefaultServiceType, APIKeys: []string{"fake-new"}, ModelCapabilities: map[string]UpstreamModelCapability{"m": {Pricing: &ModelPricing{CacheCreation5mPrice: &price}}}}, ChannelPlacementFront)
				if err == nil {
					t.Fatal("failed Add accepted")
				}
				if !reflect.DeepEqual(before, cm.config) {
					t.Fatalf("failed Add changed live channels or priorities: %+v", kind.Slice(cm.config))
				}
				readPath := path
				if failure == "persistence" {
					readPath = filepath.Join(path, "existing")
				}
				after, err := os.ReadFile(readPath)
				if err != nil || !bytes.Equal(data, after) {
					t.Fatal("failed Add changed durable config")
				}
			})
		}
	}
}
