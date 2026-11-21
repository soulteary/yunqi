import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { versionService } from './version'

describe('Yunqi release source', () => {
  let storage: Map<string, string>
  beforeEach(() => {
    storage = new Map()
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => storage.set(key, value),
      removeItem: (key: string) => storage.delete(key),
    })
    versionService.setCurrentVersion('v3.0.0')
  })
  afterEach(() => vi.unstubAllGlobals())

  it('ignores upstream release cache when checking the fork', async () => {
    storage.set('ccx-version-info', JSON.stringify({
      currentVersion: 'v3.0.0', latestVersion: 'v99.0.0',
      releaseUrl: 'https://github.com/BenedictKing/ccx/releases/tag/v99.0.0',
      lastCheckTime: Date.now(), status: 'update-available', hasUpdate: true,
    }))
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify([{
      tag_name: 'v3.0.1', html_url: 'https://github.com/soulteary/yunqi/releases/tag/v3.0.1',
      published_at: '2026-11-21', name: 'Yunqi',
    }])))
    vi.stubGlobal('fetch', fetchMock)
    const result = await versionService.checkForUpdates()
    expect(fetchMock.mock.calls[0]?.[0]).toBe('https://api.github.com/repos/soulteary/yunqi/releases?per_page=10')
    expect(result.releaseUrl).toBe('https://github.com/soulteary/yunqi/releases/tag/v3.0.1')
    expect(storage.has('yunqi-version-info')).toBe(true)
  })

  it('does not recommend upstream releases when the fork has no releases', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('[]')))
    const result = await versionService.checkForUpdates()
    expect(result.status).toBe('error')
    expect(result.hasUpdate).toBe(false)
    expect(result.releaseUrl).toBeNull()
  })
})
