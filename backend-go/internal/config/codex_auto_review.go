package config

import (
	"fmt"
	"strings"
	"unicode"
)

const CodexAutoReviewModel = "codex-auto-review"

// CodexAutoReviewMapping maps only the reserved review request model. The target
// goes through normal routing and capability checks, independently of approval policy.
type CodexAutoReviewMapping struct {
	Enabled     bool   `json:"enabled"`
	TargetModel string `json:"targetModel"`
}

func (mapping CodexAutoReviewMapping) Validate() error {
	if mapping.TargetModel != strings.TrimSpace(mapping.TargetModel) {
		return fmt.Errorf("目标模型不能包含首尾空白")
	}
	if mapping.Enabled && mapping.TargetModel == "" {
		return fmt.Errorf("启用映射时必须填写目标模型")
	}
	if len(mapping.TargetModel) > 256 || strings.IndexFunc(mapping.TargetModel, unicode.IsControl) >= 0 {
		return fmt.Errorf("目标模型过长或含控制字符")
	}
	if strings.EqualFold(mapping.TargetModel, CodexAutoReviewModel) {
		return fmt.Errorf("目标模型不能是 codex-auto-review 自身")
	}
	return nil
}

func (cm *ConfigManager) GetCodexAutoReviewMapping() CodexAutoReviewMapping {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config.CodexAutoReview
}

func (cm *ConfigManager) SetCodexAutoReviewMappingAudited(auditContext ManagementAuditContext, mapping CodexAutoReviewMapping) error {
	auditContext = auditContext.normalized()

	mapping.TargetModel = strings.TrimSpace(mapping.TargetModel)
	if err := mapping.Validate(); err != nil {
		return err
	}
	cm.mu.Lock()
	previous := cm.config.CodexAutoReview
	cm.config.CodexAutoReview = mapping
	if err := cm.saveConfigLockedAudited(auditContext, cm.config); err != nil {
		cm.config.CodexAutoReview = previous
		cm.mu.Unlock()
		return err
	}
	cm.fireConfigChangeCallbacks()
	return nil
}

// SetCodexAutoReviewMapping preserves the system/background call contract. HTTP callers use SetCodexAutoReviewMappingAudited.
func (cm *ConfigManager) SetCodexAutoReviewMapping(mapping CodexAutoReviewMapping) error {
	return cm.SetCodexAutoReviewMappingAudited(ManagementAuditContext{}, mapping)
}
