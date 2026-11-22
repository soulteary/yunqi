package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/soulteary/yunqi/internal/presetstore"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func shadowPriceServer(t *testing.T, bundle *presetstore.PresetBundle) *httptest.Server {
	t.Helper()
	files := map[string]any{"subscriptionPreset": bundle.Subscription, "modelRegistry": bundle.ModelRegistry, "channelPresets": bundle.ChannelPresets, "builtinManifest": bundle.BuiltinModelsManifests}
	bodies := map[string][]byte{}
	index := presetstore.PresetIndex{SchemaVersion: 1, DataVersion: bundle.DataVersion, PublishedAt: time.Now().UTC()}
	for kind, value := range files {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := "/" + kind + ".json"
		bodies[path] = data
		hash := sha256.Sum256(data)
		index.Shards = append(index.Shards, presetstore.PresetIndexShard{Kind: kind, URL: "." + path, SHA256: hex.EncodeToString(hash[:])})
	}
	data, _ := json.Marshal(index)
	bodies["/index.json"] = data
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if data, ok := bodies[r.URL.Path]; ok {
			_, _ = w.Write(data)
		} else {
			http.NotFound(w, r)
		}
	}))
}
func TestPresetNilPatternsCannotSilentlyShadowRuntimePricing(t *testing.T) {
	original := presetstore.Default()
	defer func() { presetstore.SetDefault(original); rebuildBuiltinSnapshotForStore(original) }()
	for _, duplicate := range []bool{false, true} {
		base := presetstore.EmbeddedBundle()
		base.DataVersion = "100"
		price := 2.0
		base.ModelRegistry.UpstreamCapabilities = []presetstore.ModelRegistryCapabilityPreset{{Patterns: []string{"^gpt-.*"}, Pricing: &presetstore.ModelPricingPreset{InputCacheMissPrice: &price}}}
		store := presetstore.NewPresetStore(base)
		presetstore.SetDefault(store)
		before := ResolveUpstreamCapability("gpt-4o", nil, nil)
		if before.Capability.Pricing == nil || *before.Capability.Pricing.InputCacheMissPrice != 2 {
			t.Fatal("runtime wildcard price fixture failed")
		}
		incoming := store.Get()
		incoming.DataVersion = "101"
		pattern := "gpt-4o"
		if duplicate {
			pattern = "^gpt-.*"
		}
		incoming.ModelRegistry.UpstreamCapabilities = append(incoming.ModelRegistry.UpstreamCapabilities, presetstore.ModelRegistryCapabilityPreset{Patterns: []string{pattern}, Pricing: nil})
		server := shadowPriceServer(t, incoming)
		updater := presetstore.NewPresetUpdater(store, presetstore.UpdaterConfig{IndexURL: server.URL + "/index.json", CacheDir: t.TempDir(), AllowInsecureForTesting: true})
		err := updater.CheckOnce(context.Background())
		server.Close()
		still := ResolveUpstreamCapability("gpt-4o", nil, nil)
		if still.Capability.Pricing == nil || *still.Capability.Pricing.InputCacheMissPrice != 2 {
			t.Fatal("unapproved nil pattern erased the actual runtime price")
		}
		state := updater.PriceReviewState()
		if duplicate {
			if err == nil || !strings.Contains(err.Error(), "duplicate") || state.Pending != nil {
				t.Fatalf("nil duplicate was not rejected: %v %+v", err, state)
			}
			continue
		}
		if err != nil || state.Pending == nil || state.Pending.ChangeCount != 1 || !state.Pending.RequiresAnomalyAcknowledgement {
			t.Fatalf("exact nil shadow was not held for review: %v %+v", err, state)
		}
		if err := updater.ApprovePriceReview("test-owner", state.Pending.ReviewID, true); err != nil {
			t.Fatal(err)
		}
		actual := ResolveUpstreamCapability("gpt-4o", nil, nil)
		if actual.MatchedPattern != "gpt-4o" || actual.Capability.Pricing != nil {
			t.Fatal("test did not exercise the actual exact-over-wildcard resolver shadow")
		}
	}
}
