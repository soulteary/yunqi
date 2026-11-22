package config

import "reflect"

// checkDiscoverySourceLocked binds automatic writes to the exact configuration
// used for discovery, including the absence of an administrator review policy.
func (cm *ConfigManager) checkDiscoverySourceLocked(source *UpstreamConfig) error {
	if source == nil {
		return ErrModelImportConflict
	}
	current := cm.modelImportChannelLocked(source.ChannelUID)
	if current == nil || current.ModelImport != nil || modelImportRevision(current) != modelImportRevision(source) {
		return ErrModelImportConflict
	}
	return nil
}

// UpdateDiscoveryChannel applies one automatic write only while its discovery
// source and target still permit legacy automation. Advance the caller snapshot
// exclusively from this successful transaction, never from an unrelated reload.
func (cm *ConfigManager) UpdateDiscoveryChannel(source *UpstreamConfig, targetUID string, updates UpstreamUpdate) error {
	allowed := UpstreamUpdate{SupportedModels: updates.SupportedModels, LearnedClientFingerprint: updates.LearnedClientFingerprint, Status: updates.Status}
	if !reflect.DeepEqual(allowed, updates) {
		return invalidModelImport("automatic discovery only updates model rules, fingerprint or status")
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if err := cm.checkDiscoverySourceLocked(source); err != nil {
		return err
	}
	for _, kind := range orderedChannelKinds() {
		k := ChannelKindRegistry[kind]
		list := k.Slice(cm.config)
		for i := range list {
			if list[i].ChannelUID == targetUID {
				if list[i].ModelImport != nil || (targetUID != source.ChannelUID && (source.AccountUID == "" || list[i].AccountUID != source.AccountUID)) {
					return ErrModelImportConflict
				}
				if _, err := cm.updateUpstreamCommonLocked(k, i, updates); err != nil {
					return err
				}
				*source = *cm.modelImportChannelLocked(source.ChannelUID).Clone()
				return nil
			}
		}
	}
	return ErrModelImportNotFound
}

// ApplyDiscoveryAccountChannelChanges guards an automatic protocol expansion in
// the same lock as its complete account transaction.
func (cm *ConfigManager) ApplyDiscoveryAccountChannelChanges(source *UpstreamConfig, accountUID string, updates []AccountChannelUpdate, additions []AccountChannelAddition) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if err := cm.checkDiscoverySourceLocked(source); err != nil {
		return err
	}
	if source.AccountUID == "" || source.AccountUID != accountUID {
		return ErrModelImportConflict
	}
	for _, update := range updates {
		current := cm.modelImportChannelLocked(update.ChannelUID)
		if current == nil || current.ModelImport != nil {
			return ErrModelImportConflict
		}
	}
	if err := cm.applyAccountChannelChangesLocked(accountUID, updates, additions); err != nil {
		return err
	}
	*source = *cm.modelImportChannelLocked(source.ChannelUID).Clone()
	return nil
}
