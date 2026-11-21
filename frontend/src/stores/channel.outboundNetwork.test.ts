// @vitest-environment jsdom
import { createPinia, setActivePinia } from 'pinia'
import { expect, it, vi } from 'vitest'
import type { Channel } from '../services/api'
import { useChannelStore } from './channel'

const mocks = vi.hoisted(() => ({ updateChannelV2: vi.fn(), updateManagedAccount: vi.fn() }))
vi.mock('@/services/api', () => ({ api: mocks }))
vi.mock('@/services/autopilot-api', () => ({ autoAddChannel: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ currentRoute: { value: { params: { type: 'responses' } } } }) }))
vi.mock('@/stores/preferences', () => ({ usePreferencesStore: () => ({ uiLanguage: 'zh-CN' }) }))
vi.mock('@/i18n', () => ({ translate: (_locale: string, key: string) => key }))

const changes = [
  { outboundNetwork: { proxyMode: 'direct' } },
  { outboundNetwork: null },
  { proxyPreferDirect: true },
  { proxyPreferDirect: false },
  { proxyUrl: 'http://localhost:7891', outboundNetwork: { proxyMode: 'proxy' } },
] as const

it.each(changes.flatMap(change => [false, true].map(provider => ({ change, provider }))))('托管渠道保存出口配置 %o', async ({ change, provider }) => {
  setActivePinia(createPinia())
  mocks.updateChannelV2.mockReset().mockResolvedValue(undefined)
  mocks.updateManagedAccount.mockReset().mockResolvedValue({})
  const original = {
    index: 0, channelUid: 'audit', name: 'test', baseUrl: 'https://example.invalid',
    apiKeys: [], serviceType: 'responses', autoManaged: true,
    outboundNetwork: { proxyMode: 'legacy' },
    proxyPreferDirect: 'proxyPreferDirect' in change && change.proxyPreferDirect === false,
    ...(provider ? { providerId: 'provider' } : {}),
  } as Channel
  const store = useChannelStore()
  store.responsesChannelsData = { channels: [{ ...original, channelUid: 'other' }, { ...original, index: 1 }], current: -1 }
  const result = await store.saveChannel({ ...original, ...change } as Channel, 0, {
    channelType: 'responses', autoManaged: true, accountUid: 'account', originalChannel: original,
  })
  expect(result.success).toBe(true)
  expect(mocks.updateChannelV2).toHaveBeenCalledExactlyOnceWith('audit', expect.objectContaining(change))
})
