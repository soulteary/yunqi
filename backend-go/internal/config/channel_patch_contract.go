package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type channelPatchField struct {
	name   string
	shared bool
}

// Identity/lifecycle fields require explicit concrete values. Optional mutable
// settings accept null as resetting their override, including inherited booleans.
var concreteChannelFields = map[string]bool{
	"name": true, "serviceType": true, "baseUrl": true, "baseUrls": true, "apiKeys": true, "apiKeyConfigs": true,
	"status": true, "priority": true, "promotionUntil": true, "autoManaged": true, "autoManagedAt": true,
	"autoManagedKind": true, "learnedClientFingerprint": true, "outboundNetwork": true,
}
var sharedResetFields = map[string]bool{
	"authHeader": true, "remark": true, "description": true, "website": true, "insecureSkipVerify": true,
	"lowQuality": true, "autoBlacklistBalance": true, "normalizeMetadataUserId": true, "customHeaders": true,
	"proxyUrl": true, "proxyPreferDirect": true, "requestTimeoutMs": true, "responseHeaderTimeoutMs": true,
	"streamFirstContentTimeoutMs": true, "streamInactivityTimeoutMs": true, "streamToolCallIdleTimeoutMs": true,
	"costMultiplier": true, "channelPaymentCurrency": true, "channelPaymentAmount": true, "channelCreditCurrency": true,
	"channelCreditAmount": true, "maxGroupMultiplier": true, "racing": true,
}

func channelPatchMetadata() map[string]channelPatchField {
	result := make(map[string]channelPatchField)
	update, target := reflect.TypeOf(UpstreamUpdate{}), reflect.TypeOf(UpstreamConfig{})
	for i := 0; i < update.NumField(); i++ {
		field := update.Field(i)
		key := strings.Split(field.Tag.Get("json"), ",")[0]
		if key == "" || key == "-" || concreteChannelFields[key] {
			continue
		}
		if _, found := target.FieldByName(field.Name); found {
			result[key] = channelPatchField{name: field.Name, shared: sharedResetFields[key]}
		}
	}
	return result
}

// ParseChannelNullFields is shared by legacy and unified update decoders.
func ParseChannelNullFields(data []byte) ([]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	metadata := channelPatchMetadata()
	var fields []string
	for key, value := range raw {
		if !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		if key == "outboundNetwork" {
			continue
		} // dedicated presence-aware network patch
		if concreteChannelFields[key] {
			return nil, fmt.Errorf("%s cannot be null; omit it to retain its value", key)
		}
		if _, ok := metadata[key]; ok {
			fields = append(fields, key)
		}
	}
	return fields, nil
}
func (u *UpstreamUpdate) UnmarshalJSON(data []byte) error {
	type wire UpstreamUpdate
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	fields, err := ParseChannelNullFields(data)
	if err != nil {
		return err
	}
	*u = UpstreamUpdate(decoded)
	u.NullFields = fields
	return nil
}
func applyChannelNullFields(channel *UpstreamConfig, fields []string) {
	metadata := channelPatchMetadata()
	target := reflect.ValueOf(channel).Elem()
	for _, key := range fields {
		if field, ok := metadata[key]; ok {
			value := target.FieldByName(field.name)
			value.Set(reflect.Zero(value.Type()))
		}
	}
}
