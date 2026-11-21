import { describe, expect, it } from 'vitest'
import type { Channel } from '../services/api'
import { extractEditChannelPayloadFields } from './editChannelPayload'

describe('extractEditChannelPayloadFields', () => {
  it('保留 apiKeyConfigs 的完整服务端元数据与扩展字段', () => {
    const apiKeyConfigs = [{
      key: 'key-a',
      keyUid: 'uid-a',
      credentialUid: 'credential-a',
      groupMultiplier: null,
      maxGroupMultiplier: 0,
      multiplierSource: 'subscription',
      consumptionPolicy: 'opportunistic' as const,
      effectiveCostClass: 'zero' as const,
      multiplierUpdatedAt: 'updated',
      multiplierExpiresAt: 'expires',
      multiplierSyncStatus: 'ok',
      multiplierSyncError: '',
      sourceSubscriptionUid: 'subscription-a',
      sourceRemoteTokenId: 'remote-a',
      eligible: false,
      ineligibleReason: 'expired',
      futureField: { preserved: true },
    }]
    const channel = { name: 'channel', apiKeyConfigs } as unknown as Channel

    const result = extractEditChannelPayloadFields(channel)

    expect(result.apiKeyConfigs).toBe(apiKeyConfigs)
    expect(result.apiKeyConfigs).toEqual(apiKeyConfigs)
  })

  it('保留 consumptionPolicy 三态与 effectiveCostClass', () => {
    const configs = [
      { key: 'key-a', consumptionPolicy: undefined },
      { key: 'key-b', consumptionPolicy: 'normal' as const },
      { key: 'key-c', consumptionPolicy: 'opportunistic' as const, effectiveCostClass: 'zero' as const },
    ]
    const channel = { name: 'channel', apiKeyConfigs: configs } as unknown as Channel

    const result = extractEditChannelPayloadFields(channel)

    expect(result.apiKeyConfigs).toEqual(configs)
  })
})

it('editing a name retains optional configuration and concrete false/zero/null values', () => {
  const channel = {
    name: 'before', fastMode: false, requestTimeoutMs: 0, outboundNetwork: null,
    customHeaders: { 'x-extra': 'keep' }, balanceCheck: { enabled: false },
  } as unknown as Channel
  expect({ ...extractEditChannelPayloadFields(channel), name: 'after' }).toEqual({
    ...channel, name: 'after',
  })
})
