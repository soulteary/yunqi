package messages

import (
	"sort"

	"github.com/gofiber/fiber/v3"
	"github.com/soulteary/yunqi/internal/config"
	"github.com/soulteary/yunqi/internal/httpbridge"
	"github.com/soulteary/yunqi/internal/scheduler"
)

// Decorate a copy at response time. Cached upstream data never contains aliases,
// so editing or clearing a mapping takes effect during a discovery outage.
func withClientModelAliases(c fiber.Ctx, manager *config.ConfigManager, response ModelsResponse) ModelsResponse {
	cfg := manager.GetConfig()
	if httpbridge.Param(c, "routePrefix") != "" || cfg.ClientModelMappings == nil {
		return response
	}
	result := cloneModelsResponse(response)
	if _, configured := cfg.ClientModelMappings["claude-desktop"]; configured || len(cfg.ClientModelMappings) == 0 {
		for i := range result.Data {
			result.Data[i].AnthropicFamilyTier = ""
			result.Data[i].IsFamilyDefault = false
		}
	}
	// Other clients select their aliases explicitly. Exposing their Claude-like
	// names here would let Desktop infer tiers from a different client's profile.
	slots := cfg.ClientModelMappings["claude-desktop"]
	ordered := make([]string, 0, len(slots))
	for slot := range slots {
		ordered = append(ordered, slot)
	}
	sort.Strings(ordered)
	for _, slot := range ordered {
		alias := config.ClientMappingAlias("claude-desktop", slot)
		target, reserved := manager.ResolveClientModelAlias(alias, "messages")
		if !reserved || target == "" {
			continue
		}
		model, eligible := configuredClientMappingModel(c, cfg, target, "messages")
		if !eligible {
			continue
		}
		// Discovery intentionally samples a bounded number of channels. A configured
		// target outside that sample still needs a discoverable, routable slot.
		for _, live := range response.Data {
			if live.ID == target {
				model = live
				break
			}
		}
		result.Data = append(result.Data, clientAliasEntry(model, "claude-desktop", slot))
	}
	return buildModelsResponse(result.Data)
}

func configuredClientMappingModel(c fiber.Ctx, cfg config.Config, target, kind string) (ModelEntry, bool) {
	if httpbridge.Param(c, "routePrefix") != "" {
		return ModelEntry{}, false
	}
	for _, up := range config.ChannelKindRegistry[config.ChannelKind(kind)].Slice(cfg) {
		if up.Status != "active" || up.RoutePrefix != "" || len(up.APIKeys) == 0 || !up.SupportsModel(target) {
			continue
		}
		if pin := httpbridge.GetHeader(c, "X-Channel"); pin != "" && pin != up.Name {
			continue
		}
		endpoint := config.NormalizeEndpointType(up.ServiceType)
		if up.ServiceType == "copilot" {
			endpoint = config.NormalizeEndpointType(up.CopilotProtocol)
		}
		model := normalizeModelEntry(ModelEntry{ID: target, EndpointMetadata: []config.ModelEndpointMetadata{{Type: endpoint, Source: "configured"}}}, &up, cfg.UpstreamModelCapabilities)
		return model, true
	}
	return ModelEntry{}, false
}

func clientAliasDetail(c fiber.Ctx, manager *config.ConfigManager, sch *scheduler.ChannelScheduler, alias string) (ModelEntry, bool) {
	cfg := manager.GetConfig()
	for _, kind := range []string{"messages", "responses", "chat"} {
		target, reserved := manager.ResolveClientModelAlias(alias, kind)
		if !reserved || target == "" {
			continue
		}
		model, eligible := configuredClientMappingModel(c, cfg, target, kind)
		if !eligible {
			continue
		}
		request := modelsCollectionRequest{ctx: httpbridge.Request(c).Context(), cfgManager: manager, channelScheduler: sch, channelName: httpbridge.GetHeader(c, "X-Channel")}
		for _, live := range collectModelsFromChannels(request, scheduler.ChannelKind(kind), modelsMaxChannels) {
			if live.ID == target {
				model = live
				break
			}
		}
		for client, slots := range manager.GetClientModelMappings() {
			for slot := range slots {
				if config.ClientMappingAlias(client, slot) == alias {
					return clientAliasEntry(model, client, slot), true
				}
			}
		}
	}
	return ModelEntry{}, false
}

func clientAliasEntry(model ModelEntry, client, slot string) ModelEntry {
	target := model.ID
	alias := config.ClientMappingAlias(client, slot)
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
