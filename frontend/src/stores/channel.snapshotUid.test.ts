// @vitest-environment jsdom
import { createPinia, setActivePinia } from 'pinia'
import { expect, it, vi } from 'vitest'
import type { Channel } from '../services/api'
import { useChannelStore } from './channel'

const mocks = vi.hoisted(() => ({ updateChannelV2: vi.fn() }))
vi.mock('@/services/api', () => ({ api: mocks }))
vi.mock('@/services/autopilot-api', () => ({ autoAddChannel: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ currentRoute: { value: { params: { type: 'responses' } } } }) }))
vi.mock('@/stores/preferences', () => ({ usePreferencesStore: () => ({ uiLanguage: 'zh-CN' }) }))
vi.mock('@/i18n', () => ({ translate: (_locale: string, key: string) => key }))

it.each(['messages', 'chat', 'responses', 'gemini', 'images', 'vectors'] as const)(
  '%s 渠道列表重排后保存仍定位编辑时的 UID', async kind => {
    setActivePinia(createPinia())
    mocks.updateChannelV2.mockReset().mockResolvedValue(undefined)
    const store = useChannelStore()
    const original = {
      index: 0, channelUid: 'ch_original', name: 'original',
      baseUrl: 'https://example.invalid', apiKeys: [], serviceType: 'openai',
    } as Channel
    const other = { ...original, channelUid: 'ch_other', name: 'other' }
    const data = { channels: [other, { ...original, index: 1 }], current: -1 }
    switch (kind) {
      case 'messages': store.channelsData = data; break
      case 'chat': store.chatChannelsData = data; break
      case 'responses': store.responsesChannelsData = data; break
      case 'gemini': store.geminiChannelsData = data; break
      case 'images': store.imagesChannelsData = data; break
      case 'vectors': store.vectorsChannelsData = data; break
    }
    await store.saveChannel({ ...original, name: 'edited original' }, 0, {
      channelType: kind, originalChannel: original, autoManaged: false,
    })
    expect(mocks.updateChannelV2).toHaveBeenCalledExactlyOnceWith('ch_original', expect.objectContaining({ name: 'edited original' }))
  },
)
