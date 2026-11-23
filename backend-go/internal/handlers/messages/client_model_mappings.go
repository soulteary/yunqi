package messages

import (
	"sort"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
)

// Decorate a copy at response time. Cached upstream data never contains aliases,
// so editing or clearing a mapping takes effect even during a discovery outage.
func withClientModelAliases(c fiber.Ctx, manager *config.ConfigManager, response ModelsResponse) ModelsResponse {
	if httpbridge.Param(c, "routePrefix") != "" || manager.GetConfig().ClientModelMappings == nil {
		return response
	}
	result := cloneModelsResponse(response)
	mappings := manager.GetClientModelMappings()
	// Once explicitly configured, Desktop tiers come only from its selected slots.
	if _, configured := mappings["claude-desktop"]; configured || len(mappings) == 0 {
		for i := range result.Data {
			result.Data[i].AnthropicFamilyTier = ""
			result.Data[i].IsFamilyDefault = false
		}
	}
	clients := make([]string, 0, len(mappings))
	for client := range mappings {
		clients = append(clients, client)
	}
	sort.Strings(clients)
	for _, client := range clients {
		if client != "claude-desktop" {
			continue
		}
		slots := make([]string, 0, len(mappings[client]))
		for slot := range mappings[client] {
			slots = append(slots, slot)
		}
		sort.Strings(slots)
		for _, slot := range slots {
			alias := config.ClientMappingAlias(client, slot)
			target, ok := manager.ResolveClientModelAlias(alias, config.ClientMappingKind(client))
			if !ok || target == "" {
				continue
			}
			for _, model := range response.Data {
				if model.ID != target {
					continue
				}
				model = clientAliasEntry(model, client, slot)
				result.Data = append(result.Data, model)
				break
			}
		}
	}
	return buildModelsResponse(result.Data)
}

func clientAliasDetail(c fiber.Ctx, manager *config.ConfigManager, sch *scheduler.ChannelScheduler, alias string) (ModelEntry, bool) {
	if httpbridge.Param(c, "routePrefix") != "" {
		return ModelEntry{}, false
	}
	cfg := manager.GetConfig()
	for _, kind := range []string{"messages", "responses", "chat"} {
		target, reserved := manager.ResolveClientModelAlias(alias, kind)
		if !reserved || target == "" {
			continue
		}
		request := modelsCollectionRequest{ctx: httpbridge.Request(c).Context(), cfgManager: manager, channelScheduler: sch, channelName: httpbridge.GetHeader(c, "X-Channel")}
		liveModels := collectModelsFromChannels(request, scheduler.ChannelKind(kind), modelsMaxChannels)
		var live *ModelEntry
		for _, model := range liveModels {
			if model.ID == target {
				value := model
				live = &value
				break
			}
		}
		for _, up := range config.ChannelKindRegistry[config.ChannelKind(kind)].Slice(cfg) {
			if up.Status != "active" || up.RoutePrefix != "" || !up.SupportsModel(target) || len(up.APIKeys) == 0 {
				continue
			}
			if pin := httpbridge.GetHeader(c, "X-Channel"); pin != "" && pin != up.Name {
				continue
			}
			model := normalizeModelEntry(ModelEntry{ID: target, EndpointMetadata: []config.ModelEndpointMetadata{{Type: kind, Source: "configured"}}}, &up, cfg.UpstreamModelCapabilities)
			if live != nil {
				model = *live
			}
			for client, slots := range manager.GetClientModelMappings() {
				for slot := range slots {
					if config.ClientMappingAlias(client, slot) == alias {
						return clientAliasEntry(model, client, slot), true
					}
				}
			}
		}
	}
	return ModelEntry{}, false
}

func clientAliasEntry(model ModelEntry, client, slot string) ModelEntry {
	alias := config.ClientMappingAlias(client, slot)
	target := model.ID
	model.ID = alias
	model.Name = alias
	model.DisplayName = target + " (" + client + " / " + slot + ")"
	model.LabelOverride = model.DisplayName
	model.AnthropicFamilyTier = ""
	model.IsFamilyDefault = false
	if client == "claude-desktop" {
		model.AnthropicFamilyTier = slot
		model.IsFamilyDefault = true
	}
	return model
}
