package messages

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/soulteary/cache-kit/v2/ttlcache"
	"github.com/soulteary/yunqi/internal/config"
)

func TestModelsDiscoveryCacheCopiesResponses(t *testing.T) {
	cache := newModelsDiscoveryCache()
	key := modelsDiscoveryCacheKey{importRevision: "revision", routePrefix: "route", channelName: "channel"}
	response := ModelsResponse{Object: "list", Data: []ModelEntry{{ID: "model", InputModalities: []string{"text"}, SupportedEndpointTypes: []string{"chat"}, EndpointMetadata: []config.ModelEndpointMetadata{{Type: "chat", Source: "approved", Verified: true}}}}}
	cache.Set(key, response)
	response.Data[0].ID = "changed"
	response.Data[0].InputModalities[0] = "changed"
	response.Data[0].SupportedEndpointTypes[0] = "changed"
	response.Data[0].EndpointMetadata[0].Source = "changed"
	for i := 0; i < 2; i++ {
		got, ok := cache.Get(key)
		if !ok || len(got.Data) != 1 {
			t.Fatalf("cache miss: %+v", got)
		}
		model := got.Data[0]
		if model.ID != "model" || model.InputModalities[0] != "text" || model.SupportedEndpointTypes[0] != "chat" || model.EndpointMetadata[0].Source != "approved" {
			t.Fatalf("response aliases cached data: %+v", model)
		}
		got.Data[0].ID = "returned mutation"
		got.Data[0].InputModalities[0] = "returned mutation"
		got.Data[0].SupportedEndpointTypes[0] = "returned mutation"
		got.Data[0].EndpointMetadata[0].Source = "returned mutation"
	}
}

func TestModelsDiscoveryCacheSeparatesSelectionAndRevision(t *testing.T) {
	cache := newModelsDiscoveryCache()
	key := modelsDiscoveryCacheKey{importRevision: "revision", routePrefix: "route", channelName: "channel"}
	cache.Set(key, ModelsResponse{Object: "list"})
	for _, other := range []modelsDiscoveryCacheKey{
		{importRevision: "new revision", routePrefix: "route", channelName: "channel"},
		{importRevision: "revision", routePrefix: "other route", channelName: "channel"},
		{importRevision: "revision", routePrefix: "route", channelName: "other channel"},
	} {
		if _, ok := cache.Get(other); ok {
			t.Errorf("unexpected hit for %+v", other)
		}
	}
}

func TestModelsDiscoveryCacheConcurrentAccess(t *testing.T) {
	cache := newModelsDiscoveryCache()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := modelsDiscoveryCacheKey{channelName: fmt.Sprint(i)}
			for j := 0; j < 20; j++ {
				cache.Set(key, ModelsResponse{Data: []ModelEntry{{ID: key.channelName}}})
				got, ok := cache.Get(key)
				if !ok || len(got.Data) != 1 || got.Data[0].ID != key.channelName {
					t.Errorf("unexpected cached response: %+v, found=%v", got, ok)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestModelsDiscoveryCacheExpirationBoundary(t *testing.T) {
	clock := ttlcache.NewManualClock(time.Unix(1700000000, 0))
	cache := newModelsDiscoveryCacheWithClock(clock)
	key := modelsDiscoveryCacheKey{channelName: "expired"}
	cache.Set(key, ModelsResponse{Object: "list"})
	clock.Advance(modelsDiscoveryCacheTTL)
	if _, ok := cache.Get(key); !ok {
		t.Fatal("entry expired at the inclusive deadline")
	}
	clock.Advance(time.Nanosecond)
	if _, ok := cache.Get(key); ok {
		t.Fatal("entry survived after its deadline")
	}
}

func TestModelsDiscoveryCacheWriteRemovesExpiredEntries(t *testing.T) {
	clock := ttlcache.NewManualClock(time.Unix(1700000000, 0))
	cache := newModelsDiscoveryCacheWithClock(clock)
	cache.Set(modelsDiscoveryCacheKey{channelName: "expired"}, ModelsResponse{})
	clock.Advance(modelsDiscoveryCacheTTL + time.Nanosecond)
	cache.Set(modelsDiscoveryCacheKey{channelName: "fresh"}, ModelsResponse{})
	if got := cache.entries.Stats().Entries; got != 1 {
		t.Fatalf("entries after write = %d, want 1", got)
	}
}
