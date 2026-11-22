package messages

import "encoding/json"

// modelEntryJSON avoids recursive calls to ModelEntry's JSON methods.
type modelEntryJSON ModelEntry

// MarshalJSON adds Desktop's discovery fields while retaining the existing API.
func (model ModelEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		modelEntryJSON
		Supports1M          bool   `json:"supports_1m,omitempty"`
		MaxInputTokens      int    `json:"max_input_tokens,omitempty"`
		AnthropicFamilyTier string `json:"anthropic_family_tier,omitempty"`
		IsFamilyDefault     bool   `json:"is_family_default,omitempty"`
	}{modelEntryJSON(model), model.Supports1M, model.ContextWindow, model.AnthropicFamilyTier, model.IsFamilyDefault})
}

// UnmarshalJSON accepts both spellings; explicit canonical values take precedence.
func (model *ModelEntry) UnmarshalJSON(data []byte) error {
	var wire struct {
		modelEntryJSON
		Supports1M          *bool   `json:"supports_1m"`
		MaxInputTokens      int     `json:"max_input_tokens"`
		AnthropicFamilyTier *string `json:"anthropic_family_tier"`
		IsFamilyDefault     *bool   `json:"is_family_default"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*model = ModelEntry(wire.modelEntryJSON)
	if wire.Supports1M != nil {
		model.Supports1M = *wire.Supports1M
	}
	if model.ContextWindow == 0 {
		model.ContextWindow = wire.MaxInputTokens
	}
	if wire.AnthropicFamilyTier != nil {
		model.AnthropicFamilyTier = *wire.AnthropicFamilyTier
	}
	if wire.IsFamilyDefault != nil {
		model.IsFamilyDefault = *wire.IsFamilyDefault
	}
	return nil
}
