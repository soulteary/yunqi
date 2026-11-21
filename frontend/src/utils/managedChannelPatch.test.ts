import { describe, expect, it } from 'vitest'
import { buildManagedChannelPatch, buildStagedKeyMultiplierConfigs, listDroppedManagedChannelFields } from './managedChannelPatch'
import type { Channel } from '../services/api'

const baseChannel = {
  name: 'managed-ch',
  apiKeys: ['sk-1'],
  costMultiplier: 2,
  maxGroupMultiplier: 1.5,
  channelPaymentCurrency: 'CNY',
  channelPaymentAmount: 100,
  channelCreditCurrency: 'USD',
  channelCreditAmount: 15,
  proxyUrl: 'http://127.0.0.1:7890',
  customHeaders: { 'x-test': '1' },
  fastMode: false,
  tags: ['a'],
} as unknown as Channel

describe('buildManagedChannelPatch', () => {
  it('保留未提供的出口字段，显式 null/false 才清除', () => {
    const original = { ...baseChannel, outboundNetwork: { proxyMode: 'direct' as const }, proxyPreferDirect: true }
    expect(buildManagedChannelPatch(original, { ...baseChannel })).toEqual({})
    expect(buildManagedChannelPatch(original, { ...original, outboundNetwork: null, proxyPreferDirect: false }))
      .toEqual({ outboundNetwork: null, proxyPreferDirect: false })
  })

  it('出口配置按结构比较，DNS 数组变化才提交', () => {
    const original = { ...baseChannel, outboundNetwork: { dns: { mode: 'bound' as const, servers: ['127.0.0.1:53'] } } }
    expect(buildManagedChannelPatch(original, structuredClone(original))).toEqual({})
    const outboundNetwork = { dns: { mode: 'bound', servers: ['127.0.0.2:53'] } }
    expect(buildManagedChannelPatch(original, { ...original, outboundNetwork })).toEqual({ outboundNetwork })
    expect(listDroppedManagedChannelFields(original, { ...original, outboundNetwork, proxyPreferDirect: true })).toEqual([])
  })
  it('无变化时返回空对象', () => {
    expect(buildManagedChannelPatch(baseChannel, { ...baseChannel })).toEqual({})
  })

  it('表单字符串与视图数字同口径不误判，真变化才进 patch', () => {
    const patch = buildManagedChannelPatch(baseChannel, {
      ...baseChannel,
      maxGroupMultiplier: '1.5',
      costMultiplier: '3',
    })
    expect(patch).toEqual({ costMultiplier: 3 })
  })

  it('清空表单值归一化为清除口径（0/空串/空对象）', () => {
    const patch = buildManagedChannelPatch(baseChannel, {
      ...baseChannel,
      costMultiplier: '',
      maxGroupMultiplier: null,
      channelPaymentCurrency: '  ',
      channelPaymentAmount: 0,
      proxyUrl: '',
      customHeaders: null,
    })
    expect(patch).toEqual({
      costMultiplier: 0,
      maxGroupMultiplier: 0,
      channelPaymentCurrency: '',
      channelPaymentAmount: 0,
      proxyUrl: '',
      customHeaders: {},
    })
  })

  it('customHeaders 增删键都会产生 patch（结构比较）', () => {
    expect(buildManagedChannelPatch(baseChannel, { ...baseChannel, customHeaders: { 'x-test': '1', 'x-new': '2' } }))
      .toEqual({ customHeaders: { 'x-test': '1', 'x-new': '2' } })
    expect(buildManagedChannelPatch(baseChannel, { ...baseChannel, customHeaders: {} }))
      .toEqual({ customHeaders: {} })
  })

  it('original 为空时不产生 patch', () => {
    expect(buildManagedChannelPatch(null, { costMultiplier: 3 })).toEqual({})
    expect(buildManagedChannelPatch(undefined, { costMultiplier: 3 })).toEqual({})
  })
})

describe('listDroppedManagedChannelFields', () => {
  it('账号接口与白名单字段承载的变化不报丢弃', () => {
    const dropped = listDroppedManagedChannelFields(baseChannel, {
      ...baseChannel,
      name: 'renamed',
      apiKeys: ['sk-2'],
      proxyUrl: 'http://127.0.0.2:7890',
      costMultiplier: 5,
    })
    expect(dropped).toEqual([])
  })

  it('可变设置统一由单卡patch承载', () => {
    const dropped = listDroppedManagedChannelFields(baseChannel, {
      ...baseChannel,
      fastMode: true,
      tags: ['a', 'b'],
    })
    expect(dropped).toEqual([])
    expect(buildManagedChannelPatch(baseChannel, { ...baseChannel, fastMode: true, tags: ['a', 'b'] })).toEqual({ fastMode: true, tags: ['a', 'b'] })
  })

  it('数字与数字字符串宽松相等不误报', () => {
    const dropped = listDroppedManagedChannelFields(baseChannel, {
      ...baseChannel,
      channelPaymentAmount: '100',
    })
    expect(dropped).toEqual([])
  })
})

describe('buildStagedKeyMultiplierConfigs', () => {
  const original = {
    ...baseChannel,
    apiKeyConfigs: [
      { key: 'sk-1', keyUid: 'uid-1', groupMultiplier: 1, consumptionPolicy: 'normal' },
      { key: 'sk-2', keyUid: 'uid-2', groupMultiplier: 2 },
    ],
  } as unknown as Channel

  it('倍率/策略无差异时返回 null', () => {
    expect(buildStagedKeyMultiplierConfigs(original, {
      ...original,
      apiKeyConfigs: [{ key: 'sk-1', keyUid: 'uid-1', groupMultiplier: '1', consumptionPolicy: 'normal' }],
    })).toBeNull()
  })

  it('倍率变化返回定位+倍率字段的 trimmed 配置', () => {
    const staged = buildStagedKeyMultiplierConfigs(original, {
      ...original,
      apiKeyConfigs: [
        { key: 'sk-1', keyUid: 'uid-1', groupMultiplier: 1, consumptionPolicy: 'normal' },
        { key: 'sk-2', keyUid: 'uid-2', groupMultiplier: 0.5, consumptionPolicy: 'opportunistic' },
      ],
    })
    expect(staged).toEqual([
      { key: 'sk-2', keyUid: 'uid-2', credentialUid: undefined, groupMultiplier: 0.5, consumptionPolicy: 'opportunistic' },
    ])
  })

  it('清空倍率（null）也是有效差异', () => {
    const staged = buildStagedKeyMultiplierConfigs(original, {
      ...original,
      apiKeyConfigs: [{ key: 'sk-1', keyUid: 'uid-1', groupMultiplier: null, consumptionPolicy: 'normal' }],
    })
    expect(staged).toEqual([
      { key: 'sk-1', keyUid: 'uid-1', credentialUid: undefined, groupMultiplier: null, consumptionPolicy: 'normal' },
    ])
  })

  it('original 为空或无 configs 时返回 null', () => {
    expect(buildStagedKeyMultiplierConfigs(null, { apiKeyConfigs: [{ key: 'k' }] })).toBeNull()
    expect(buildStagedKeyMultiplierConfigs(original, {})).toBeNull()
  })
})

it('every omitted managed setting preserves the server value', () => {
  expect(buildManagedChannelPatch(baseChannel, { name: 'renamed' })).toEqual({})
  expect(buildManagedChannelPatch(baseChannel, { costMultiplier: undefined, customHeaders: undefined })).toEqual({})
})

it('keeps null, false, zero and empty containers distinct', () => {
  const before = { ...baseChannel, autoBlacklistBalance: false, requestTimeoutMs: 1000, tags: ['a'], fastMode: true }
  expect(buildManagedChannelPatch(before, { autoBlacklistBalance: null, requestTimeoutMs: 0, tags: [], fastMode: false }))
    .toEqual({ autoBlacklistBalance: null, requestTimeoutMs: 0, tags: [], fastMode: false })
})

it('provider protocol defaults remain managed, generic accounts allow overrides', () => {
  const original = { ...baseChannel, providerId: 'provider', autoManagedKind: 'provider' }
  expect(buildManagedChannelPatch(original as Channel, { ...original, fastMode: true, tags: [] })).toEqual({ tags: [] })
  expect(listDroppedManagedChannelFields(original as Channel, { ...original, fastMode: true })).toEqual(['fastMode'])
  const generic = { ...original, autoManagedKind: 'generic' }
  expect(buildManagedChannelPatch(generic as Channel, { ...generic, fastMode: true })).toEqual({ fastMode: true })
})
