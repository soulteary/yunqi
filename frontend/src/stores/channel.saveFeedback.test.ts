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

const base = {
 index: 0, channelUid: 'test', name: 'test', baseUrl: 'https://example.invalid',
 apiKeys: [], serviceType: 'responses', autoManaged: true, providerId: 'provider',
} as Channel

it('does not claim success when no save request was sent', async () => {
 setActivePinia(createPinia())
 const store = useChannelStore()
 const result = await store.saveChannel({ ...base }, 0, {
  channelType: 'responses', autoManaged: true, accountUid: 'account', originalChannel: base,
 })
 expect(result.success).toBe(false)
 expect(result.message).toContain('没有发送保存请求')
})

it('does not claim complete success when managed fields were dropped', async () => {
 setActivePinia(createPinia())
 mocks.updateManagedAccount.mockResolvedValue({})
 const original = { ...base, providerId: undefined }
 const result = await useChannelStore().saveChannel({ ...original, fastMode: true }, 0, {
  channelType: 'responses', autoManaged: true, accountUid: 'account', originalChannel: original,
 })
 expect(result.success).toBe(false)
 expect(result.message).toContain('fastMode')
})
