// @vitest-environment jsdom
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiClient } from './api/client'
import { ApiService } from './api'
import { useChannelStore } from '../stores/channel'

const auth = vi.hoisted(() => ({ apiKey: 'test-key', clearAuth: vi.fn() }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/preferences', () => ({ usePreferencesStore: () => ({ uiLanguage: 'zh-CN' }) }))
vi.mock('@/i18n', () => ({ translate: (_locale: string, key: string) => key }))
vi.mock('vue-router', () => ({ useRouter: () => ({ currentRoute: { value: { params: { type: 'responses' } } } }) }))

const fetchMock = vi.fn<typeof fetch>()
let pendingSignal: AbortSignal | null | undefined

function stallUntilAborted(_url: RequestInfo | URL, options?: RequestInit): Promise<Response> {
  pendingSignal = options?.signal
  return new Promise((_resolve, reject) => {
    const signal = options?.signal
    if (signal?.aborted) reject(signal.reason)
    else signal?.addEventListener('abort', () => reject(signal.reason), { once: true })
  })
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.useFakeTimers()
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockReset()
  auth.clearAuth.mockClear()
  pendingSignal = undefined
})

afterEach(() => {
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('dashboard polling recovery', () => {
  it.each(['responses', 'images'] as const)('%s refresh releases a stalled poll and later recovers', async kind => {
    const store = useChannelStore()
    store.activeTab = kind
    fetchMock.mockImplementationOnce(stallUntilAborted)
    // Attach handlers before advancing the timer: both callers share the same in-flight work.
    const result = Promise.allSettled([store.refreshChannels(), store.refreshChannels()])
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(29_999)
    expect(pendingSignal?.aborted).toBe(false)
    await vi.advanceTimersByTimeAsync(1)
    expect(pendingSignal?.aborted).toBe(true)
    const failures = await result
    expect(failures).toEqual([
      { status: 'rejected', reason: expect.objectContaining({ name: 'TimeoutError' }) },
      { status: 'rejected', reason: expect.objectContaining({ name: 'TimeoutError' }) },
    ])
    expect(store.lastRefreshSuccess).toBe(false)
    expect(auth.clearAuth).not.toHaveBeenCalled()

    const channel = { index: 0, channelUid: 'recovered', name: 'recovered', baseUrl: 'https://example.invalid', apiKeys: [], serviceType: 'responses' }
    const dashboard = { channels: [channel], metrics: [] }
    const body = kind === 'images' ? dashboard : {
      dashboards: Object.fromEntries(['messages', 'responses', 'chat', 'gemini'].map(type => [type, dashboard])),
    }
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify(body), { status: 200 }))
    await store.refreshChannels()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(store.lastRefreshSuccess).toBe(true)
    expect((kind === 'images' ? store.imagesChannelsData : store.responsesChannelsData).channels[0]?.name).toBe('recovered')
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps the deadline active while the dashboard response body is stalled', async () => {
    fetchMock.mockImplementationOnce(async (_url, options) => {
      pendingSignal = options?.signal
      return {
        ok: true, status: 200,
        text: () => new Promise<string>((_resolve, reject) => {
          options?.signal?.addEventListener('abort', () => reject(new DOMException('body aborted', 'AbortError')), { once: true })
        }),
      } as Response
    })
    const result = expect(new ApiService().getLlmChannelDashboard()).rejects.toMatchObject({ name: 'TimeoutError' })
    await vi.advanceTimersByTimeAsync(30_000)
    await result
    expect(pendingSignal?.aborted).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
  })
})

describe('request deadline lifecycle', () => {
  it('cleans up the deadline when fetch fails before response headers', async () => {
    const error = new TypeError('network unavailable')
    fetchMock.mockRejectedValueOnce(error)
    await expect(new ApiService().getLlmChannelDashboard()).rejects.toBe(error)
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each([false, true])('preserves caller cancellation (already aborted=%s)', async alreadyAborted => {
    fetchMock.mockImplementationOnce(stallUntilAborted)
    const caller = new AbortController()
    const reason = new DOMException('caller cancelled', 'AbortError')
    if (alreadyAborted) caller.abort(reason)
    const remove = vi.spyOn(caller.signal, 'removeEventListener')
    const request = new ApiClient().request('/dashboard', { signal: caller.signal }, 30_000)
    const result = expect(request).rejects.toBe(reason)
    if (!alreadyAborted) caller.abort(reason)
    await result
    expect(pendingSignal?.aborted).toBe(true)
    expect(remove).toHaveBeenCalledWith('abort', expect.any(Function))
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each([200, 500])('cleans up the timer on HTTP %s without cancelling the caller', async status => {
    fetchMock.mockResolvedValueOnce(new Response('{}', { status }))
    const caller = new AbortController()
    const remove = vi.spyOn(caller.signal, 'removeEventListener')
    const request = new ApiClient().request('/dashboard', { signal: caller.signal }, 30_000)
    if (status === 200) await expect(request).resolves.toEqual({})
    else await expect(request).rejects.toMatchObject({ status: 500 })
    expect(remove).toHaveBeenCalledWith('abort', expect.any(Function))
    expect(caller.signal.aborted).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('does not impose dashboard deadlines on unrelated long-running requests', async () => {
    let finish!: (response: Response) => void
    fetchMock.mockImplementationOnce((_url, options) => {
      pendingSignal = options?.signal
      return new Promise(resolve => { finish = resolve })
    })
    const request = new ApiClient().request('/long-running-operation', { method: 'POST' })
    await vi.advanceTimersByTimeAsync(120_000)
    expect(pendingSignal).toBeUndefined()
    expect(vi.getTimerCount()).toBe(0)
    finish(new Response('{}', { status: 200 }))
    await expect(request).resolves.toEqual({})
  })
})
