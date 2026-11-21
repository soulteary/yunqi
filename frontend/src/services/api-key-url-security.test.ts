import { describe, expect, it, vi } from 'vitest'
import { createChannelsMessagesApi } from './api/channels/messages'
import { createChannelsChatApi } from './api/channels/chat'
import { createChannelsResponsesApi } from './api/channels/responses'
import { createChannelsGeminiApi } from './api/channels/gemini'
import { createChannelsImagesApi } from './api/channels/images'
import { createChannelsVectorsApi } from './api/channels/vectors'
import type { ApiClient } from './api/client'

const cases = [
  ['messages', createChannelsMessagesApi, ''],
  ['chat', createChannelsChatApi, 'Chat'],
  ['responses', createChannelsResponsesApi, 'Responses'],
  ['gemini', createChannelsGeminiApi, 'Gemini'],
  ['images', createChannelsImagesApi, 'Images'],
  ['vectors', createChannelsVectorsApi, 'Vectors']
] as const

describe('API key mutation URL security', () => {
  it.each(cases)('keeps %s API keys in request bodies', async (kind, factory, prefix) => {
    const request = vi.fn().mockResolvedValue({})
    const client = { request } as unknown as ApiClient
    const api = factory(client) as unknown as Record<string, (id: number, key: string) => Promise<void>>
    const key = 'DUMMY_KEY/space +percent%'
    for (const [method, action] of [
      [`remove${prefix}ApiKey`, 'remove'],
      [`move${prefix}ApiKeyToTop`, 'move-to-top'],
      [`move${prefix}ApiKeyToBottom`, 'move-to-bottom']
    ]) {
      await api[method](7, key)
      expect(request).toHaveBeenLastCalledWith(`/${kind}/channels/7/keys/${action}`, {
        method: 'POST',
        body: JSON.stringify({ apiKey: key })
      })
      const url = request.mock.lastCall?.[0] as string
      expect(url).not.toContain(key)
      expect(url).not.toContain(encodeURIComponent(key))
    }
  })
})
