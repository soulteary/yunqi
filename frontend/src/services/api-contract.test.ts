import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import defaultApi, { api, ApiError, ApiService, BALANCE_PROVIDER_VALUES } from './api'
import { BALANCE_PROVIDER_VALUES as balanceProviders } from './api-types'
import { useAuthStore } from '@/stores/auth'
import methodNames from './__fixtures__/api-methods.json'
const fetchMock = vi.fn()
function response(value: unknown = {}) {
  return Promise.resolve(new Response(JSON.stringify(value), { status: 200 }))
}
beforeEach(() => {
  setActivePinia(createPinia())
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockReset().mockImplementation(() => response())
})
describe('public API compatibility', () => {
  it('preserves all public methods and singleton exports', () => {
    expect(defaultApi).toBe(api)
    expect(api).toBeInstanceOf(ApiService)
    expect(BALANCE_PROVIDER_VALUES).toBe(balanceProviders)
    const instance = new ApiService()
    const ignored = new Set(['constructor', 'historyQuery', 't', 'getApiKey', 'parseResponseBody', 'request'])
    const methods = new Set<string>()
    let current: object | null = instance
    while (current && current !== Object.prototype) {
      for (const name of Object.getOwnPropertyNames(current)) {
        if (!ignored.has(name) && typeof (instance as unknown as Record<string, unknown>)[name] === 'function')
          methods.add(name)
      }
      current = Object.getPrototypeOf(current)
    }
    expect([...methods].sort()).toEqual(methodNames)
  })
  it('constructs independent typed instances with callable composed methods', async () => {
    const first: ApiService = new ApiService()
    const second: ApiService = new ApiService()
    expect(first).toBeInstanceOf(ApiService)
    expect(second).toBeInstanceOf(ApiService)
    expect(first).not.toBe(second)
    expect(first.getChannels).not.toBe(second.getChannels)

    // Composed methods close over their transport and remain callable when used
    // as callbacks, without sharing mutable methods with another instance.
    const { getChannels } = first
    await getChannels()
    await second.getChannels()
    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual(['/api/messages/channels', '/api/messages/channels'])
  })
  const protocols = [
    ['messages', 'addChannel', 'updateChannel', 'deleteChannel', 'getChannelMetricsHistory'],
    [
      'responses',
      'addResponsesChannel',
      'updateResponsesChannel',
      'deleteResponsesChannel',
      'getResponsesChannelMetricsHistory'
    ],
    ['chat', 'addChatChannel', 'updateChatChannel', 'deleteChatChannel', 'getChatChannelMetricsHistory'],
    ['gemini', 'addGeminiChannel', 'updateGeminiChannel', 'deleteGeminiChannel', 'getGeminiChannelMetricsHistory'],
    ['images', 'addImagesChannel', 'updateImagesChannel', 'deleteImagesChannel', 'getImagesChannelMetricsHistory'],
    ['vectors', 'addVectorsChannel', 'updateVectorsChannel', 'deleteVectorsChannel', 'getVectorsChannelMetricsHistory']
  ] as const
  it.each(protocols)(
    'keeps %s channel paths, payloads and history defaults',
    async (kind, add, update, remove, history) => {
      const service = new ApiService()
      const channel = {
        name: 'test',
        baseUrl: 'https://example.test/v1',
        apiKeys: ['test-only'],
        serviceType: 'openai' as const
      }
      await service[add](channel, 'front')
      expect(fetchMock.mock.calls[0]).toEqual([
        `/api/${kind}/channels`,
        expect.objectContaining({ method: 'POST', body: JSON.stringify({ ...channel, placement: 'front' }) })
      ])
      await service[update](3, { priority: 0 })
      expect(fetchMock.mock.calls[1]).toEqual([
        `/api/${kind}/channels/3`,
        expect.objectContaining({ method: 'PUT', body: '{"priority":0}' })
      ])
      await service[remove](3)
      expect(fetchMock.mock.calls[2]).toEqual([
        `/api/${kind}/channels/3`,
        expect.objectContaining({ method: 'DELETE' })
      ])
      await service[history]()
      expect(fetchMock.mock.calls[3][0]).toBe(`/api/${kind}/channels/metrics/history?duration=24h`)
      await service[history]('6h', '5m')
      expect(fetchMock.mock.calls[4][0]).toBe(`/api/${kind}/channels/metrics/history?duration=6h&interval=5m`)
    }
  )
  it('preserves encoded identifiers, optional fields and raw response values across domains', async () => {
    const service = new ApiService()
    await service.getChannelV2('channel/1')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/channels/channel/1')
    await service.getChannelCapabilityTestStatus('messages', 2, 'job/1')
    expect(fetchMock.mock.calls[1][0]).toBe('/api/messages/channels/2/capability-test/job/1')
    await service.setConversationOverride('conversation', [], 0, [], true)
    expect(JSON.parse(fetchMock.mock.calls[2][1].body)).toEqual({
      sequence: [],
      duration: 0,
      clearSubagentSequence: true
    })
    await service.getSubscription('subscription/1')
    expect(fetchMock.mock.calls[3][0]).toBe('/api/subscriptions/subscription%2F1')
    await service.getDeepSeekAccountBalances('account/1')
    expect(fetchMock.mock.calls[4][0]).toBe('/api/accounts/account%2F1/deepseek-balance')
    await service.getHealthCenterChannelEndpoints('channel/1')
    expect(fetchMock.mock.calls[5][0]).toBe('/api/health-center/channels/channel%2F1/endpoints')
    await service.getSmartRoutingConfig()
    expect(fetchMock.mock.calls[6][0]).toBe('/api/smart-routing/config')
    await service.getCostReport()
    expect(fetchMock.mock.calls[7][0]).toBe('/api/reports/cost?groupBy=user&duration=7d&type=messages')
    await service.getConsoleStatus()
    expect(fetchMock.mock.calls[8][0]).toBe('/api/console/status')
    await service.getCodexAutoReviewMapping()
    expect(fetchMock.mock.calls[9][0]).toBe('/api/console/codex-auto-review')
    await service.setCodexAutoReviewMapping({ enabled: true, targetModel: 'working-model' })
    expect(fetchMock.mock.calls[10][0]).toBe('/api/console/codex-auto-review')
    expect(fetchMock.mock.calls[10][1].method).toBe('PUT')
    expect(JSON.parse(fetchMock.mock.calls[10][1].body)).toEqual({ enabled: true, targetModel: 'working-model' })
  })
})
describe('shared request behavior', () => {
  it('reads the current key on every request rather than capturing it at construction', async () => {
    const service = new ApiService()
    const auth = useAuthStore()
    auth.apiKey = 'test-key-one'
    await service.getChannels()
    auth.apiKey = 'test-key-two'
    await service.getSubscriptions()
    expect(fetchMock.mock.calls[0][1].headers['x-api-key']).toBe('test-key-one')
    expect(fetchMock.mock.calls[1][1].headers['x-api-key']).toBe('test-key-two')
  })
  it('clears authentication on 401 and preserves ApiError details', async () => {
    useAuthStore().apiKey = 'test-invalid-key'
    fetchMock.mockImplementation(() => Promise.resolve(new Response('{"error":"denied"}', { status: 401 })))
    const error = await new ApiService().getChannels().catch(error => error)
    expect(error).toBeInstanceOf(ApiError)
    expect(error.status).toBe(401)
    expect(error.details).toEqual({ error: 'denied' })
    expect(useAuthStore().apiKey).toBeNull()
  })
  it.each([
    ['{"error":"specific","message":"fallback"}', 'specific'],
    ['{"message":"fallback"}', 'fallback'],
    ['plain error', 'plain error'],
    ['', 'Request failed (500)']
  ])('preserves error parsing for %s', async (body, message) => {
    fetchMock.mockImplementation(() => Promise.resolve(new Response(body, { status: 500 })))
    await expect(new ApiService().getChannels()).rejects.toMatchObject({ status: 500, message })
  })
  it('preserves JSON, non-JSON and empty successful responses', async () => {
    fetchMock
      .mockImplementationOnce(() => response({ value: 0 }))
      .mockImplementationOnce(() => Promise.resolve(new Response('plain success')))
      .mockImplementationOnce(() => Promise.resolve(new Response(null, { status: 204 })))
    const service = new ApiService()
    expect(await service.getChannels()).toEqual({ value: 0 })
    expect(await service.getChannels()).toBe('plain success')
    expect(await service.getChannels()).toBeNull()
  })
})
