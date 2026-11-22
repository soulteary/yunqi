package config

import "github.com/soulteary/yunqi/internal/pricingtypes"

func PricingVersionID(p *ModelPricing) string            { return pricingtypes.VersionID(p) }
func SnapshotModelPricing(p *ModelPricing) *ModelPricing { return pricingtypes.Snapshot(p) }
func ValidateModelPricing(p *ModelPricing) error         { return pricingtypes.Validate(p) }

// Normalize snapshots before persistence/activation, keeping optimistic channel
// revisions identical to the deep copies returned by management read APIs.
func normalizeConfigPricing(cfg *Config) error {
	check := func(caps map[string]UpstreamModelCapability) error {
		for key, capability := range caps {
			if err := ValidateModelPricing(capability.Pricing); err != nil {
				return err
			}
			capability.Pricing = SnapshotModelPricing(capability.Pricing)
			caps[key] = capability
		}
		return nil
	}
	channel := func(up *UpstreamConfig) error {
		if err := check(up.ModelCapabilities); err != nil {
			return err
		}
		if err := ValidateModelPricing(up.DefaultCapability.Pricing); err != nil {
			return err
		}
		up.DefaultCapability.Pricing = SnapshotModelPricing(up.DefaultCapability.Pricing)
		return nil
	}
	if err := check(cfg.UpstreamModelCapabilities); err != nil {
		return err
	}
	for _, kind := range ChannelKindRegistry {
		routes := kind.SliceRef(cfg)
		for i := range *routes {
			if err := channel(&(*routes)[i]); err != nil {
				return err
			}
		}
	}
	for i := range cfg.ChannelsV3 {
		for j := range cfg.ChannelsV3[i].Protocols {
			if err := channel(&cfg.ChannelsV3[i].Protocols[j].Upstream); err != nil {
				return err
			}
		}
	}
	return nil
}
