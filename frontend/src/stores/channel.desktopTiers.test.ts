// @vitest-environment jsdom
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Channel } from '../services/api'
import { useChannelStore } from './channel'

const mocks = vi.hoisted(() => ({ updateChannelV2: vi.fn(), updateManagedAccount: vi.fn() }))
vi.mock('@/services/api', () => ({ api: mocks }))
vi.mock('@/services/autopilot-api', () => ({ autoAddChannel: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ currentRoute: { value: { params: { type: 'chat' } } } }) }))
vi.mock('@/stores/preferences', () => ({ usePreferencesStore: () => ({ uiLanguage: 'zh-CN' }) }))
vi.mock('@/i18n', () => ({ translate: (_locale: string, key: string) => key }))

const original = {
  index: 0, channelUid: 'chat', name: 'DeepSeek', baseUrl: 'https://example.com', apiKeys: ['test'], serviceType: 'openai',
  protocolRoutes: [
    { kind: 'chat', index: 0, channelUid: 'chat', name: 'DeepSeek', serviceType: 'openai' },
    { kind: 'messages', index: 7, channelUid: 'messages', name: 'DeepSeek', serviceType: 'claude', claudeDesktopModelTiers: { 'deepseek-flash': 'haiku' } },
  ],
} as Channel

describe('Desktop tier save flow', () => {
  beforeEach(() => { setActivePinia(createPinia()); vi.resetAllMocks(); mocks.updateChannelV2.mockResolvedValue({ persisted: true }); mocks.updateManagedAccount.mockResolvedValue({}) })
  it.each([false, true])('saves by Messages UID from a Chat-primary card, managed=%s', async autoManaged => {
    const store = useChannelStore()
    store.chatChannelsData = { channels: [original], current: -1 }
    const next = { ...original, protocolRoutes: [original.protocolRoutes![0]!, { ...original.protocolRoutes![1]!, claudeDesktopModelTiers: { 'deepseek-flash': '', 'deepseek-v4-pro': 'sonnet' } }] }
    const result = await store.saveChannel(next, 0, { channelType: 'chat', autoManaged, accountUid: 'account', originalChannel: original })
    expect(result.success).toBe(true)
    expect(mocks.updateChannelV2).toHaveBeenCalledWith('messages', { kind: 'messages', claudeDesktopModelTiers: { 'deepseek-flash': '', 'deepseek-v4-pro': 'sonnet' } })
    const tiersPatch = mocks.updateChannelV2.mock.calls.find(([, patch]) => 'claudeDesktopModelTiers' in patch)![1]
    expect(tiersPatch).not.toHaveProperty('modelCapabilities')
    expect(original.protocolRoutes![1]!.claudeDesktopModelTiers).toEqual({ 'deepseek-flash': 'haiku' })
  })
  it('reports a tier save failure instead of closing as a success', async () => {
    const store = useChannelStore()
    store.chatChannelsData = { channels: [original], current: -1 }
    mocks.updateChannelV2.mockImplementation((uid: string) => uid === 'messages' ? Promise.reject(new Error('save failed')) : Promise.resolve({ persisted: true }))
    await expect(store.saveChannel({ ...original, protocolRoutes: [original.protocolRoutes![0]!, { ...original.protocolRoutes![1]!, claudeDesktopModelTiers: {} }] }, 0, { channelType: 'chat', originalChannel: original })).rejects.toThrow('save failed')
  })
})
