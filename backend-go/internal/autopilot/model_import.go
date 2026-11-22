package autopilot

import (
	"errors"
	"github.com/soulteary/yunqi/internal/config"
)

// stageDiscoveredModels keeps raw endpoint/key inventories intact for routing.
// The channel catalog is a separately reviewed admission layer; normalized
// public IDs never replace the provider IDs used by probes or key bindings.
func stageDiscoveredModels(uid string, channel *config.UpstreamConfig, endpoints []EndpointDiscoveryResult, cm *config.ConfigManager) error {
	candidates := make([]config.ModelImportCandidate, 0)
	for _, endpoint := range endpoints {
		if !endpoint.ProtocolOk {
			continue
		}
		observed := make(map[string][]config.ModelEndpointMetadata)
		for originalType, models := range endpoint.ProtocolModels {
			typ := config.NormalizeEndpointType(originalType)
			if typ == "" {
				continue
			}
			source := "configured"
			verified := endpoint.ProtocolDiscoverySource[originalType] == "protocol_model_probe"
			if verified {
				source = "probe"
			} else if endpoint.ProtocolDiscoverySource[originalType] == "models_api" || endpoint.ProtocolDiscoverySource[originalType] == "copilot_models_endpoint_probe" {
				source = "provider_declared"
			}
			for _, model := range models {
				observed[model] = append(observed[model], config.ModelEndpointMetadata{Type: typ, Source: source, Verified: verified})
			}
		}
		for _, model := range endpoint.Models {
			metadata := append([]config.ModelEndpointMetadata(nil), observed[model]...)
			for _, typ := range endpoint.declaredEndpointTypes[model] {
				typ = config.NormalizeEndpointType(typ)
				if typ != "" {
					metadata = append(metadata, config.ModelEndpointMetadata{Type: typ, Source: "provider_declared"})
				}
			}
			if len(metadata) == 0 {
				typ := config.NormalizeEndpointType(channel.ServiceType)
				if channel.ServiceType == "copilot" {
					typ = config.NormalizeEndpointType(channel.CopilotProtocol)
				}
				if typ != "" {
					metadata = append(metadata, config.ModelEndpointMetadata{Type: typ, Source: "configured"})
				}
			}
			candidates = append(candidates, config.ModelImportCandidate{ID: model, Endpoints: metadata})
		}
	}
	if len(candidates) == 0 {
		return errors.New("model import discovery returned no usable inventory")
	} // discovery failure is never an empty/removal proposal
	_, err := cm.StageModelImport(uid, candidates, "discovery", config.ModelImportRevision(channel))
	return err
}
