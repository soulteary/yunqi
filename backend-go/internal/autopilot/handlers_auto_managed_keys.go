package autopilot

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"

	"github.com/soulteary/yunqi/internal/config"
)

type existingChannelRef struct {
	Kind       string
	ChannelUID string
}

// degradeKeyVerifyFailure 判定新增 key 验证失败是否可降级为警告。
// 仅 *KeyVerifyError 且 AuthFailed=false（超时/网络/5xx 等非鉴权失败）可降级；
// 鉴权失败（401/403）与其他未知错误仍硬阻断。
func degradeKeyVerifyFailure(err error) (*KeyVerifyError, bool) {
	var kvErr *KeyVerifyError
	if errors.As(err, &kvErr) && !kvErr.AuthFailed {
		return kvErr, true
	}
	return nil, false
}

// verifyNewKeysForChannels 探测渠道新增 key。非鉴权类失败降级为警告返回，
// 仅鉴权失败或未知错误阻断保存；warnings 供调用方透传给用户。
func verifyNewKeysForChannels(ctx context.Context, deps *AutoManagedDeps, channels []config.AccountChannel, desiredKeys []string) ([]string, error) {
	if deps == nil {
		return nil, fmt.Errorf("自动托管依赖不可用")
	}
	if deps.SkipChannelKeyVerify {
		return nil, nil
	}
	verify := deps.VerifyChannelKey
	if verify == nil {
		verify = verifyChannelKey
	}
	var warnings []string
	for _, accountChannel := range channels {
		existing := make(map[string]struct{}, len(accountChannel.Upstream.APIKeys))
		for _, apiKey := range accountChannel.Upstream.APIKeys {
			existing[apiKey] = struct{}{}
		}
		for _, apiKey := range uniqueNonEmptyStrings(desiredKeys) {
			if _, ok := existing[apiKey]; ok {
				continue
			}
			if err := verify(ctx, accountChannel.Kind, accountChannel.Upstream, apiKey); err != nil {
				if kvErr, ok := degradeKeyVerifyFailure(err); ok {
					warning := fmt.Sprintf("key %s 已保存，但连通性验证未通过：%s。该 key 可能仍可用于实际调用，保活验证将持续检查", kvErr.MaskedKey, kvErr.Error())
					log.Printf("[AutoManaged-Verify] 降级放行: %s", warning)
					warnings = append(warnings, warning)
					continue
				}
				return warnings, err
			}
		}
	}
	return warnings, nil
}

// verifyAddedKeysForUpdates 探测账号更新中新增 key。降级策略同 verifyNewKeysForChannels。
func verifyAddedKeysForUpdates(ctx context.Context, deps *AutoManagedDeps, channels []config.AccountChannel, updates []config.AccountChannelUpdate) ([]string, error) {
	if deps == nil {
		return nil, fmt.Errorf("自动托管依赖不可用")
	}
	if deps.SkipChannelKeyVerify {
		return nil, nil
	}
	verify := deps.VerifyChannelKey
	if verify == nil {
		verify = verifyChannelKey
	}
	var warnings []string
	byUID := make(map[string]config.AccountChannelUpdate, len(updates))
	for _, update := range updates {
		byUID[update.ChannelUID] = update
	}
	for _, accountChannel := range channels {
		update, ok := byUID[accountChannel.Upstream.ChannelUID]
		if !ok {
			continue
		}
		existing := make(map[string]struct{}, len(accountChannel.Upstream.APIKeys))
		for _, apiKey := range accountChannel.Upstream.APIKeys {
			existing[apiKey] = struct{}{}
		}
		upstream := accountChannel.Upstream
		upstream.APIKeys = append([]string(nil), update.APIKeys...)
		upstream.APIKeyConfigs = append([]config.APIKeyConfig(nil), update.APIKeyConfig...)
		upstream.BaseURLs = append([]string(nil), update.BaseURLs...)
		if len(upstream.BaseURLs) > 0 {
			upstream.BaseURL = upstream.BaseURLs[0]
		}
		for _, apiKey := range uniqueNonEmptyStrings(update.APIKeys) {
			if _, ok := existing[apiKey]; ok {
				continue
			}
			if err := verify(ctx, accountChannel.Kind, upstream, apiKey); err != nil {
				if kvErr, ok := degradeKeyVerifyFailure(err); ok {
					warning := fmt.Sprintf("key %s 已保存，但连通性验证未通过：%s。该 key 可能仍可用于实际调用，保活验证将持续检查", kvErr.MaskedKey, kvErr.Error())
					log.Printf("[AutoManaged-Verify] 降级放行: %s", warning)
					warnings = append(warnings, warning)
					continue
				}
				return warnings, err
			}
		}
	}
	return warnings, nil
}

func channelsWithAddedKeys(channels []config.AccountChannel, desiredKeys []string) []existingChannelRef {
	desired := uniqueNonEmptyStrings(desiredKeys)
	refs := make([]existingChannelRef, 0, len(channels))
	for _, accountChannel := range channels {
		existing := make(map[string]struct{}, len(accountChannel.Upstream.APIKeys))
		for _, apiKey := range accountChannel.Upstream.APIKeys {
			existing[apiKey] = struct{}{}
		}
		added := false
		for _, apiKey := range desired {
			if _, ok := existing[apiKey]; !ok {
				added = true
				break
			}
		}
		if added {
			refs = append(refs, existingChannelRef{Kind: accountChannel.Kind, ChannelUID: accountChannel.Upstream.ChannelUID})
		}
	}
	return refs
}

func resetChangedChannelMetrics(deps *AutoManagedDeps, refs []existingChannelRef) {
	if deps == nil || deps.CfgManager == nil || deps.ResetChannelMetrics == nil {
		return
	}
	cfg := deps.CfgManager.GetConfig()
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		key := ref.Kind + "\x00" + ref.ChannelUID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		index := findChannelIndexByUID(getChannelSlice(cfg, ref.Kind), ref.ChannelUID)
		if index >= 0 {
			deps.ResetChannelMetrics(ref.Kind, index)
		}
	}
}

func apiKeysEqual(left, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}
