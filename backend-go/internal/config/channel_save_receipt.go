package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

var ErrChannelRevisionConflict = errors.New("channel configuration changed; reload before saving")

// ChannelSaveReceipt describes the configuration committed by this save. Revision
// is opaque and never contains configuration values or credentials.
type ChannelSaveReceipt struct {
	Revision      string   `json:"revision"`
	Persisted     bool     `json:"persisted"`
	Changed       bool     `json:"changed"`
	ChangedFields []string `json:"changedFields"`
}

func ChannelRevision(channel UpstreamConfig) string {
	data, _ := json.Marshal(channel)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// UpdateChannelWithReceipt resolves identity, optionally compares the caller's
// revision, commits and reads the resulting revision under a single write lock.
func (cm *ConfigManager) UpdateChannelWithReceipt(uid, expectedRevision string, updates UpstreamUpdate) (ChannelSaveReceipt, ChannelLocation, bool, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for _, kind := range orderedChannelKinds() {
		k := ChannelKindRegistry[kind]
		routes := k.Slice(cm.config)
		for i := range routes {
			if routes[i].ChannelUID != uid {
				continue
			}
			loc := ChannelLocation{Kind: kind, Index: i}
			beforeChannel := routes[i].Clone()
			before := ChannelRevision(routes[i])
			if expectedRevision != "" && strings.Trim(expectedRevision, "\"") != before {
				return ChannelSaveReceipt{}, loc, false, ErrChannelRevisionConflict
			}
			reset, err := cm.updateUpstreamCommonLocked(k, i, updates)
			if err != nil {
				return ChannelSaveReceipt{}, loc, false, err
			}
			after := ChannelRevision(k.Slice(cm.config)[i])
			return ChannelSaveReceipt{Revision: after, Persisted: true, Changed: before != after, ChangedFields: changedChannelFields(*beforeChannel, k.Slice(cm.config)[i])}, loc, reset, nil
		}
	}
	return ChannelSaveReceipt{}, ChannelLocation{}, false, fmt.Errorf("channel not found: %s", uid)
}

// The receipt returns field names only: keys, headers and URLs never enter it.
func changedChannelFields(before, after UpstreamConfig) []string {
	fields := make([]string, 0)
	a, b := reflect.ValueOf(before), reflect.ValueOf(after)
	typ := a.Type()
	for i := 0; i < a.NumField(); i++ {
		if !a.Field(i).CanInterface() {
			continue
		}
		if reflect.DeepEqual(a.Field(i).Interface(), b.Field(i).Interface()) {
			continue
		}
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			fields = append(fields, name)
		}
	}
	return fields
}
