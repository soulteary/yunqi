// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { computed, defineComponent, nextTick } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAppController } from './useAppController'
import { useAuthStore } from '@/stores/auth'
import { useChannelStore } from '@/stores/channel'
import { usePreferencesStore } from '@/stores/preferences'
import { api, ApiError, fetchHealth } from '@/services/api'
import { versionService, type VersionInfo } from '@/services/version'
import publicKeys from './__fixtures__/app-controller-keys.json'

const theme = vi.hoisted(() => {
  // Node 26 may expose an unavailable localStorage before jsdom installs it.
  const entries = new Map<string, string>()
  const storage: Storage = {
    get length() {
      return entries.size
    },
    key: index => [...entries.keys()][index] ?? null,
    getItem: key => entries.get(key) ?? null,
    setItem: (key, value) => {
      entries.set(key, value)
    },
    removeItem: key => {
      entries.delete(key)
    },
    clear: () => entries.clear()
  }
  Object.defineProperty(window, 'localStorage', { value: storage, configurable: true })
  return { change: vi.fn() }
})
vi.mock('vuetify', () => ({ useTheme: () => theme }))
vi.mock('@/services/api', async importOriginal => ({
  ...(await importOriginal<typeof import('@/services/api')>()),
  fetchHealth: vi.fn(),
  api: { getChannels: vi.fn() }
}))
vi.mock('@/services/version', () => ({
  versionService: { setCurrentVersion: vi.fn(), checkForUpdates: vi.fn() }
}))
vi.mock('@/i18n', async importOriginal => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => {
    const preferences = usePreferencesStore()
    return {
      locale: computed(() => preferences.uiLanguage),
      t: (key: string, params?: Record<string, string | number>) =>
        `${preferences.uiLanguage}:${key}${params ? JSON.stringify(params) : ''}`,
      setLocale: preferences.setUILanguage
    }
  }
}))

const latest: VersionInfo = {
  currentVersion: 'v3.0.0',
  latestVersion: 'v3.0.0',
  isLatest: true,
  hasUpdate: false,
  releaseUrl: null,
  lastCheckTime: 123,
  status: 'latest'
}
let wrappers: VueWrapper[] = []
let mediaListeners: Set<EventListener>
let dark = false

async function render(key?: string, refreshSuccess = true) {
  let controller!: ReturnType<typeof useAppController>
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/channels/:type', component: { template: '<div />' } }]
  })
  await router.push('/channels/messages')
  const pinia = createPinia()
  const wrapper = mount(
    defineComponent({
      setup() {
        const auth = useAuthStore()
        if (key) auth.setApiKey(key)
        const channels = useChannelStore()
        vi.spyOn(channels, 'refreshChannels').mockImplementation(async () => {
          channels.lastRefreshSuccess = refreshSuccess
        })
        vi.spyOn(channels, 'startAutoRefresh').mockImplementation(() => {})
        vi.spyOn(channels, 'stopAutoRefresh').mockImplementation(() => {})
        vi.spyOn(channels, 'clearChannels')
        controller = useAppController()
        return () => null
      }
    }),
    { global: { plugins: [pinia, router] } }
  )
  wrappers.push(wrapper)
  await flushPromises()
  return { controller, wrapper, router }
}

beforeEach(() => {
  vi.clearAllMocks()
  window.localStorage.clear()
  mediaListeners = new Set()
  dark = false
  vi.stubGlobal(
    'matchMedia',
    vi.fn(() => ({
      get matches() {
        return dark
      },
      addEventListener: (_: string, cb: EventListener) => mediaListeners.add(cb),
      removeEventListener: (_: string, cb: EventListener) => mediaListeners.delete(cb)
    }))
  )
  vi.mocked(api.getChannels).mockReset().mockResolvedValue({ channels: [], current: -1 })
  vi.mocked(fetchHealth)
    .mockReset()
    .mockResolvedValue({ version: { version: 'v3.0.0', buildTime: '', gitCommit: '' } } as Awaited<
      ReturnType<typeof fetchHealth>
    >)
  vi.mocked(versionService.checkForUpdates).mockReset().mockResolvedValue(latest)
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})
afterEach(() => {
  wrappers.forEach(wrapper => wrapper.unmount())
  wrappers = []
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('app controller compatibility', () => {
  it('preserves its public contract and shows login without starting refresh when no key is saved', async () => {
    const { controller: c } = await render()
    expect(Object.keys(c).sort()).toEqual(publicKeys)
    expect(c.showAuthDialog.value).toBe(true)
    expect(c.authStore.isAutoAuthenticating).toBe(false)
    expect(c.authStore.authError).toContain('enterAccessKeyContinue')
    expect(c.channelStore.refreshChannels).not.toHaveBeenCalled()
    expect(c.channelStore.startAutoRefresh).not.toHaveBeenCalled()
  })
  it('restores a saved key, refreshes channels and starts polling', async () => {
    const { controller: c } = await render('fixture-key')
    expect(api.getChannels).toHaveBeenCalledOnce()
    expect(c.showAuthDialog.value).toBe(false)
    expect(c.channelStore.refreshChannels).toHaveBeenCalledOnce()
    expect(c.channelStore.startAutoRefresh).toHaveBeenCalledOnce()
    expect(c.systemStore.systemStatus).toBe('running')
  })
  it('removes a saved key only on 401 and returns to login', async () => {
    vi.mocked(api.getChannels).mockRejectedValue(new ApiError('unauthorized', 401))
    const { controller: c } = await render('fixture-key')
    expect(c.authStore.apiKey).toBe(null)
    expect(window.localStorage.getItem('proxyAccessKey')).toBe(null)
    expect(c.showAuthDialog.value).toBe(true)
    expect(c.authStore.authError).toContain('savedKeyInvalid')
  })
  it('keeps a saved key on network errors and reflects failed refresh in system status', async () => {
    vi.mocked(api.getChannels).mockRejectedValue(new Error('offline'))
    const { controller: c } = await render('fixture-key', false)
    expect(c.authStore.apiKey).toBe('fixture-key')
    expect(c.systemStore.systemStatus).toBe('error')
    expect(c.toasts.value.some(toast => toast.type === 'warning')).toBe(true)
  })
  it('trims manual credentials, clears successful login input, opens the guide once, and logs out', async () => {
    const { controller: c } = await render()
    c.authStore.setAuthKeyInput('  fixture-key  ')
    await c.handleAuthSubmit()
    await nextTick()
    expect(c.authStore.apiKey).toBe('fixture-key')
    expect(c.authStore.authKeyInput).toBe('')
    expect(c.authStore.authLoading).toBe(false)
    expect(c.systemStore.systemStatus).toBe('running')
    expect(c.showGuide.value).toBe(true)
    c.showGuide.value = false
    await nextTick()
    expect(c.preferencesStore.hasSeenGuide).toBe(true)
    c.handleLogout()
    await nextTick()
    expect(c.channelStore.clearChannels).toHaveBeenCalledOnce()
    expect(c.channelStore.stopAutoRefresh).toHaveBeenCalled()
    expect(c.showAuthDialog.value).toBe(true)
    c.authStore.setAuthKeyInput('fixture-key')
    await c.handleAuthSubmit()
    await nextTick()
    expect(c.showGuide.value).toBe(false)
    c.openGuide()
    expect(c.showGuide.value).toBe(true)
  })
  it('rejects blank input and locks repeated 401s for five minutes, then permits retry after expiry', async () => {
    const { controller: c } = await render()
    await c.handleAuthSubmit()
    expect(api.getChannels).not.toHaveBeenCalled()
    vi.mocked(api.getChannels).mockRejectedValue(new ApiError('unauthorized', 401))
    c.authStore.setAuthKeyInput('bad-fixture-key')
    for (let i = 0; i < c.MAX_AUTH_ATTEMPTS; i++) await c.handleAuthSubmit()
    expect(c.authStore.authAttempts).toBe(5)
    expect(c.authStore.authLockoutTime! - Date.now()).toBeGreaterThan(299000)
    await c.handleAuthSubmit()
    expect(api.getChannels).toHaveBeenCalledTimes(5)
    c.authStore.setAuthLockout(new Date(Date.now() - 1000))
    vi.mocked(api.getChannels).mockResolvedValue({ channels: [], current: -1 })
    await c.handleAuthSubmit()
    expect(c.authStore.authAttempts).toBe(0)
    expect(c.authStore.authLockoutTime).toBe(null)
    expect(c.isAuthenticated.value).toBe(true)
  })
  it('does not count server errors as invalid manual credentials', async () => {
    const { controller: c } = await render()
    vi.mocked(api.getChannels).mockRejectedValue(new ApiError('unavailable', 503))
    c.authStore.setAuthKeyInput('fixture-key')
    await c.handleAuthSubmit()
    expect(c.authStore.apiKey).toBe('fixture-key')
    expect(c.authStore.authAttempts).toBe(0)
    expect(c.toasts.value.some(toast => toast.type === 'error')).toBe(true)
  })
  it('updates translated navigation and route-driven channel refresh without refreshing after logout', async () => {
    const { controller: c, router } = await render('fixture-key')
    c.setLocale('zh-CN')
    await nextTick()
    expect(c.currentLanguageShortLabel.value).toBe('中')
    expect(c.translatedApiTabOptions.value.find(tab => tab.value === 'console')?.label).toBe('zh-CN:app.tabs.console')
    await router.push('/channels/images')
    await flushPromises()
    expect(c.route.path).toBe('/channels/images')
    expect(c.channelStore.activeTab).toBe('images')
    expect(c.channelStore.refreshChannels).toHaveBeenCalledTimes(2)
    c.handleLogout()
    await nextTick()
    vi.mocked(c.channelStore.refreshChannels).mockClear()
    await router.push('/channels/vectors')
    await flushPromises()
    expect(c.channelStore.refreshChannels).not.toHaveBeenCalled()
  })
  it('initializes the theme, follows system changes only in auto mode, and cleans up on unmount', async () => {
    const { controller: c, wrapper } = await render()
    expect(document.documentElement.dataset.theme).toBe('retro')
    expect(document.documentElement.style.getPropertyValue('--app-font')).toContain('Courier New')
    expect(theme.change).toHaveBeenLastCalledWith('light')
    dark = true
    mediaListeners.forEach(listener => listener(new Event('change')))
    expect(theme.change).toHaveBeenLastCalledWith('dark')
    c.toggleDarkMode()
    expect(c.preferencesStore.darkModePreference).toBe('dark')
    c.toggleDarkMode()
    expect(c.preferencesStore.darkModePreference).toBe('light')
    theme.change.mockClear()
    mediaListeners.forEach(listener => listener(new Event('change')))
    expect(theme.change).not.toHaveBeenCalled()
    expect(mediaListeners.size).toBe(1)
    wrapper.unmount()
    wrappers = wrappers.filter(w => w !== wrapper)
    expect(mediaListeners.size).toBe(0)
    expect(c.channelStore.stopAutoRefresh).toHaveBeenCalled()
  })
  it('checks versions on mount and custom events, opens the dialog and removes event listeners on unmount', async () => {
    const { controller: c, wrapper } = await render()
    expect(versionService.setCurrentVersion).toHaveBeenCalledWith('v3.0.0')
    expect(c.systemStore.versionInfo).toEqual(latest)
    expect(c.systemStore.isCheckingVersion).toBe(false)
    c.handleVersionClick()
    expect(c.systemStore.updateDialogOpen).toBe(true)
    window.dispatchEvent(new Event('ccx-check-version'))
    await flushPromises()
    expect(fetchHealth).toHaveBeenCalledTimes(2)
    wrapper.unmount()
    wrappers = wrappers.filter(w => w !== wrapper)
    window.dispatchEvent(new Event('ccx-check-version'))
    await flushPromises()
    expect(fetchHealth).toHaveBeenCalledTimes(2)
  })
  it('ignores overlapping version requests while a check is pending', async () => {
    let resolveHealth!: (value: Awaited<ReturnType<typeof fetchHealth>>) => void
    vi.mocked(fetchHealth).mockImplementation(
      () =>
        new Promise(resolve => {
          resolveHealth = resolve
        })
    )
    const { controller: c } = await render()
    expect(c.systemStore.isCheckingVersion).toBe(true)
    window.dispatchEvent(new Event('ccx-check-version'))
    window.dispatchEvent(new Event('ccx-check-version'))
    expect(fetchHealth).toHaveBeenCalledOnce()
    resolveHealth({ version: { version: 'v3.0.0', buildTime: '', gitCommit: '' } } as Awaited<
      ReturnType<typeof fetchHealth>
    >)
    await flushPromises()
    expect(c.systemStore.isCheckingVersion).toBe(false)
    expect(c.systemStore.versionInfo).toEqual(latest)
  })
  it.each(['missing-version', 'health-error', 'release-error'])(
    'handles %s without retaining the checking state',
    async failure => {
      if (failure === 'missing-version')
        vi.mocked(fetchHealth).mockResolvedValue({} as Awaited<ReturnType<typeof fetchHealth>>)
      if (failure === 'health-error') vi.mocked(fetchHealth).mockRejectedValue(new Error('offline'))
      if (failure === 'release-error') vi.mocked(versionService.checkForUpdates).mockRejectedValue(new Error('offline'))
      const { controller: c } = await render()
      expect(c.systemStore.versionInfo.status).toBe('error')
      expect(c.systemStore.isCheckingVersion).toBe(false)
      if (failure !== 'release-error') expect(versionService.checkForUpdates).not.toHaveBeenCalled()
    }
  )
})
