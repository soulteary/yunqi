package config

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

// ClientModelMappings gives each client its own stable request aliases. Targets
// are actual models; ordinary admission, capabilities and scheduling still apply.
type ClientModelMappings map[string]map[string]string

type ClientMappingTarget struct {
	Model    string   `json:"model"`
	Kind     string   `json:"kind"`
	Channels []string `json:"channels"`
}

var clientMappingSlots = map[string][]string{
	"claude-desktop": {"haiku", "sonnet", "opus", "fable", "mythos"},
	"claude-code":    {"haiku", "sonnet", "opus", "fable", "mythos"},
	"codex":          {"default"},
	"opencode":       {"default", "small"},
}

func ClientMappingKind(client string) string {
	switch client {
	case "claude-desktop", "claude-code":
		return "messages"
	case "codex":
		return "responses"
	case "opencode":
		return "chat"
	}
	return ""
}
func ClientMappingAlias(client, slot string) string { return "yunqi-" + client + "-" + slot }
func CloneClientModelMappings(input ClientModelMappings) ClientModelMappings {
	if input == nil {
		return nil
	}
	result := make(ClientModelMappings, len(input))
	for client, slots := range input {
		result[client] = maps.Clone(slots)
	}
	return result
}
func (cm *ConfigManager) GetClientModelMappings() ClientModelMappings {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return effectiveClientMappings(cm.config)
}

// Only active default-route channels contribute targets. Prefix-only channels
// retain their access boundary and are never exposed through a global alias.
func clientMappingTargets(cfg Config) []ClientMappingTarget {
	targets := map[string]*ClientMappingTarget{}
	for _, kind := range []string{"messages", "responses", "chat"} {
		for _, up := range ChannelKindRegistry[ChannelKind(kind)].Slice(cfg) {
			if up.Status != "active" || up.RoutePrefix != "" || len(up.APIKeys) == 0 {
				continue
			}
			candidates := slices.Clone(up.SupportedModels)
			for model := range up.ClaudeDesktopModelTiers {
				candidates = append(candidates, model)
			}
			for _, key := range up.APIKeyConfigs {
				if key.Enabled == nil || *key.Enabled {
					candidates = append(candidates, key.Models...)
				}
			}
			for id := range up.ModelCapabilities {
				candidates = append(candidates, id)
			}
			if up.ModelImportEnabled() {
				candidates = nil
				for _, model := range up.ModelImport.Approved {
					candidates = append(candidates, model.ID)
				}
			}
			for _, model := range candidates {
				if !validClientMappingModel(model) || !up.SupportsModel(model) {
					continue
				}
				key := kind + ":" + model
				target := targets[key]
				if target == nil {
					target = &ClientMappingTarget{Model: model, Kind: kind}
					targets[key] = target
				}
				if !slices.Contains(target.Channels, up.Name) {
					target.Channels = append(target.Channels, up.Name)
				}
			}
		}
	}
	result := make([]ClientMappingTarget, 0, len(targets))
	for _, target := range targets {
		sort.Strings(target.Channels)
		result = append(result, *target)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Model < result[j].Model
	})
	return result
}
func validClientMappingModel(model string) bool {
	if model == "" || strings.TrimSpace(model) != model || strings.HasPrefix(model, "yunqi-") || strings.ContainsAny(model, "*!\r\n\t") {
		return false
	}
	_, err := NormalizeClaudeDesktopModelTiers(map[string]string{model: ""})
	return err == nil
}
func (cm *ConfigManager) ClientMappingTargets() []ClientMappingTarget {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return clientMappingTargets(cm.config)
}
func (cm *ConfigManager) SetClientModelMappingsAudited(audit ManagementAuditContext, input ClientModelMappings) error {
	if input == nil {
		return fmt.Errorf("mappings must be an object")
	}
	next := CloneClientModelMappings(input)
	cm.mu.Lock()
	targets := clientMappingTargets(cm.config)
	for client, slots := range next {
		allowed, ok := clientMappingSlots[client]
		if !ok {
			cm.mu.Unlock()
			return fmt.Errorf("unsupported client: %s", client)
		}
		for slot, model := range slots {
			if !slices.Contains(allowed, slot) || !validClientMappingModel(model) {
				cm.mu.Unlock()
				return fmt.Errorf("invalid mapping: %s/%s", client, slot)
			}
			// Existing selections survive a temporarily unavailable channel. New choices
			// must be admitted by an active channel of the client's protocol.
			previous := cm.config.ClientModelMappings[client][slot]
			if model != previous && !slices.ContainsFunc(targets, func(t ClientMappingTarget) bool { return t.Kind == ClientMappingKind(client) && t.Model == model }) {
				cm.mu.Unlock()
				return fmt.Errorf("target is not available in active %s channels: %s", ClientMappingKind(client), model)
			}
		}
	}
	previous := cm.config.ClientModelMappings
	cm.config.ClientModelMappings = next
	if err := cm.saveConfigLockedAudited(audit.normalized(), cm.config); err != nil {
		cm.config.ClientModelMappings = previous
		cm.mu.Unlock()
		return err
	}
	cm.fireConfigChangeCallbacks()
	return nil
}

// ResolveClientModelAlias recognizes only the reserved client aliases. Missing,
// disabled or protocol-mismatched selections fail closed, without auto mapping.
func (cm *ConfigManager) ResolveClientModelAlias(alias, kind string) (target string, reserved bool) {
	for client, slots := range clientMappingSlots {
		for _, slot := range slots {
			if alias != ClientMappingAlias(client, slot) {
				continue
			}
			if kind != ClientMappingKind(client) {
				return "", true
			}
			cm.mu.RLock()
			defer cm.mu.RUnlock()
			model := effectiveClientMappings(cm.config)[client][slot]
			for _, candidate := range clientMappingTargets(cm.config) {
				if candidate.Kind == kind && candidate.Model == model {
					return model, true
				}
			}
			return "", true
		}
	}
	return "", strings.HasPrefix(alias, "yunqi-claude-desktop-") || strings.HasPrefix(alias, "yunqi-claude-code-") || strings.HasPrefix(alias, "yunqi-codex-") || strings.HasPrefix(alias, "yunqi-opencode-")
}

// Project exact legacy Desktop tiers into the new editor until the first save.
// The original channel configuration remains available to older deployments.
func effectiveClientMappings(cfg Config) ClientModelMappings {
	if cfg.ClientModelMappings != nil {
		return CloneClientModelMappings(cfg.ClientModelMappings)
	}
	result := ClientModelMappings{}
	targets := clientMappingTargets(cfg)
	for _, up := range cfg.Upstream {
		if up.Status != "active" || up.RoutePrefix != "" {
			continue
		}
		tiers := ClaudeDesktopModelTiersForView(up)
		models := make([]string, 0, len(tiers))
		for model := range tiers {
			models = append(models, model)
		}
		sort.Strings(models)
		for _, model := range models {
			tier := tiers[model]
			if tier == "" || !slices.ContainsFunc(targets, func(t ClientMappingTarget) bool { return t.Kind == "messages" && t.Model == model }) {
				continue
			}
			if result["claude-desktop"] == nil {
				result["claude-desktop"] = map[string]string{}
			}
			if result["claude-desktop"][tier] == "" {
				result["claude-desktop"][tier] = model
			}
		}
	}
	return result
}
