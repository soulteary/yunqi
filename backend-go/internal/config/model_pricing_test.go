package config

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
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
