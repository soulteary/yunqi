package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

const MaxBodyLogAuditEntries = 1000
const DefaultBodyLogMaxBytes = 4096

type BodyLoggingSettings struct {
	RequestBodies  bool `json:"requestBodies"`
	ResponseBodies bool `json:"responseBodies"`
	MaxBytes       int  `json:"maxBytes"`
}
type BodyLogAuditEvent struct {
	ID      string              `json:"id"`
	At      time.Time           `json:"at"`
	ActorID string              `json:"actorId"`
	Before  BodyLoggingSettings `json:"before"`
	After   BodyLoggingSettings `json:"after"`
}
type BodyLoggingState struct {
	Configured BodyLoggingSettings `json:"configured"`
	Runtime    BodyLoggingSettings `json:"runtime"`
	Revision   string              `json:"revision"`
	Audit      []BodyLogAuditEvent `json:"audit"`
}

var bodyLoggingRuntime atomic.Pointer[BodyLoggingSettings]
var ErrBodyLogRevisionConflict = errors.New("body logging configuration changed; reload before saving")

func normalizedBodyLogging(s BodyLoggingSettings) BodyLoggingSettings {
	if s.MaxBytes == 0 {
		s.MaxBytes = DefaultBodyLogMaxBytes
	}
	return s
}
func RuntimeBodyLogging() BodyLoggingSettings {
	if s := bodyLoggingRuntime.Load(); s != nil {
		return *s
	}
	return normalizedBodyLogging(BodyLoggingSettings{})
}
func (e *EnvConfig) ShouldLogRequestBodies() bool {
	return e != nil && e.EnableRequestLogs && e.IsDevelopment() && RuntimeBodyLogging().RequestBodies
}
func (e *EnvConfig) ShouldLogResponseBodies() bool {
	return e != nil && e.EnableResponseLogs && e.IsDevelopment() && RuntimeBodyLogging().ResponseBodies
}
func bodyLoggingRevision(settings BodyLoggingSettings, audit []BodyLogAuditEvent) string {
	id := ""
	if len(audit) > 0 {
		id = audit[len(audit)-1].ID
	}
	data, _ := json.Marshal(struct {
		Settings    BodyLoggingSettings
		LastAuditID string
	}{normalizedBodyLogging(settings), id})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (cm *ConfigManager) bodyLoggingStateLocked() BodyLoggingState {
	return BodyLoggingState{Configured: normalizedBodyLogging(cm.config.BodyLogging), Runtime: RuntimeBodyLogging(), Revision: bodyLoggingRevision(cm.config.BodyLogging, cm.config.BodyLogAudit), Audit: append([]BodyLogAuditEvent(nil), cm.config.BodyLogAudit...)}
}
func (cm *ConfigManager) GetBodyLoggingState() BodyLoggingState {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.bodyLoggingStateLocked()
}

// UpdateBodyLogging commits settings and the actor audit in one config write.
// Runtime activation happens only after that durable commit succeeds.
func (cm *ConfigManager) UpdateBodyLogging(actor, expectedRevision string, settings BodyLoggingSettings) (BodyLoggingState, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.updateBodyLoggingLocked(actor, expectedRevision, settings)
}
func (cm *ConfigManager) updateBodyLoggingLocked(actor, expectedRevision string, settings BodyLoggingSettings) (BodyLoggingState, error) {
	settings = normalizedBodyLogging(settings)
	if strings.TrimSpace(actor) == "" {
		return cm.bodyLoggingStateLocked(), errors.New("body logging changes require an audit actor")
	}
	if settings.MaxBytes < 256 || settings.MaxBytes > 65536 {
		return cm.bodyLoggingStateLocked(), errors.New("body logging maxBytes must be between 256 and 65536")
	}
	if expectedRevision != "" && strings.Trim(expectedRevision, "\"") != bodyLoggingRevision(cm.config.BodyLogging, cm.config.BodyLogAudit) {
		return cm.bodyLoggingStateLocked(), ErrBodyLogRevisionConflict
	}
	before := RuntimeBodyLogging()
	if before == settings && normalizedBodyLogging(cm.config.BodyLogging) == settings {
		return cm.bodyLoggingStateLocked(), nil
	}
	original := cm.config.deepCopy()
	now := time.Now().UTC()
	event := BodyLogAuditEvent{ID: fmt.Sprintf("body_%d", now.UnixNano()), At: now, ActorID: actor, Before: before, After: settings}
	cm.config.BodyLogging = settings
	cm.config.BodyLogAudit = append(append([]BodyLogAuditEvent(nil), cm.config.BodyLogAudit...), event)
	if len(cm.config.BodyLogAudit) > MaxBodyLogAuditEntries {
		cm.config.BodyLogAudit = cm.config.BodyLogAudit[len(cm.config.BodyLogAudit)-MaxBodyLogAuditEntries:]
	}
	if err := cm.saveConfigLocked(cm.config); err != nil {
		cm.config = original
		return cm.bodyLoggingStateLocked(), err
	}
	active := settings
	bodyLoggingRuntime.Store(&active)
	return cm.bodyLoggingStateLocked(), nil
}

// Startup and file reloads must use the same audited activation path. A failed
// audit write leaves runtime content logging disabled or at its prior setting.
func (cm *ConfigManager) ActivateBodyLogging(actor string) (BodyLoggingState, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.updateBodyLoggingLocked(actor, "", cm.config.BodyLogging)
}
