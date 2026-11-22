package config

import (
	"errors"
	"github.com/soulteary/yunqi/internal/entrybudget"
	"strings"
)

func (cm *ConfigManager) PrincipalBudgetsSnapshot() []entrybudget.Policy {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return append([]entrybudget.Policy(nil), cm.config.PrincipalBudgets...)
}

// Only explicit stable principals are budget subjects. Legacy display masks and
// a client-supplied principal header can never select a different ledger.
func (cm *ConfigManager) FindPrincipalBudget(principal string) (entrybudget.Policy, bool) {
	if !strings.HasPrefix(principal, "principal:") {
		return entrybudget.Policy{}, false
	}
	id := strings.TrimPrefix(principal, "principal:")
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	for _, p := range cm.config.PrincipalBudgets {
		if p.PrincipalID == id {
			return p, true
		}
	}
	return entrybudget.Policy{}, false
}
func (cm *ConfigManager) PutPrincipalBudgetAudited(auditContext ManagementAuditContext, p entrybudget.Policy) error {
	auditContext = auditContext.normalized()

	p, err := p.Normalize()
	if err != nil {
		return err
	}
	cm.mu.Lock()
	next := cm.config.deepCopy()
	found := false
	for i, existing := range next.PrincipalBudgets {
		if existing.PrincipalID == p.PrincipalID {
			next.PrincipalBudgets[i] = p
			found = true
			break
		}
	}
	if !found {
		if len(next.PrincipalBudgets) >= 4096 {
			cm.mu.Unlock()
			return errors.New("principal budget limit reached")
		}
		next.PrincipalBudgets = append(next.PrincipalBudgets, p)
	}
	if err := cm.saveConfigLockedAudited(auditContext, next); err != nil {
		cm.mu.Unlock()
		return err
	}
	cm.fireConfigChangeCallbacks()
	return nil
}

// PutPrincipalBudget preserves the system/background call contract. HTTP callers use PutPrincipalBudgetAudited.
func (cm *ConfigManager) PutPrincipalBudget(p entrybudget.Policy) error {
	return cm.PutPrincipalBudgetAudited(ManagementAuditContext{}, p)
}
