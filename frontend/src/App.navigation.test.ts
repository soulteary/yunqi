// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { expect, it, vi } from 'vitest'
import App from './App.vue'

// Keep the real shell and RouterLink behavior, isolating unrelated authentication,
// polling and modal effects. No desktop runtime or native bridge is installed.
vi.mock('./composables/useAppController', async () => {
  const { useRoute } = await import('vue-router')
  return { useAppController: () => ({
    route: useRoute(),
    theme: { global: { current: { value: { dark: false } } } },
    authStore: { isInitialized: true, isAutoAuthenticating: false },
    channelStore: { activeTab: 'messages', currentChannelsData: { channels: [] } },
    preferencesStore: { showGlobalStats: false },
    dialogStore: {},
    systemStore: { versionInfo: {}, systemStatus: 'running' },
    t: (key: string) => key,
    languageOptions: [],
    translatedApiTabOptions: [],
    currentLocale: 'zh-CN',
    currentLanguageShortLabel: '中',
    toasts: [],
    cbForm: {},
    cbPresets: [],
    isAuthenticated: false,
    showAuthDialog: false,
    showGuide: false,
    circuitBreakerDialogOpen: false,
  }) }
})
vi.mock('./composables/useCapabilityTestManager', () => ({ useCapabilityTestManager: () => ({}) }))

const slot = defineComponent({ template: '<div><slot /></div>' })
const hiddenModal = defineComponent({ setup: () => () => null })

it('all desktop-width Web navigation links remain clickable without a native menu runtime', async () => {
  expect('go' in window).toBe(false)
  expect('runtime' in window).toBe(false)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { template: '<main data-page>{{ $route.path }}</main>' } }],
  })
  await router.push('/channels/messages')
  const wrapper = mount(App, {
    global: {
      plugins: [router],
      mocks: { $vuetify: { display: { mobile: false, width: 1280 } } },
      renderStubDefaultSlot: true,
      stubs: {
        VApp: slot, VAppBar: slot, VMain: slot, VContainer: slot,
        VProgressCircular: true, VCard: slot, VCardTitle: slot, VCardText: slot, VCardActions: slot,
        VAlert: slot, VTextField: true, VBtn: slot, VForm: slot, VDivider: true,
        VListItemTitle: slot, VListItem: slot, VList: slot, VMenu: slot,
        VSpacer: true, VIcon: slot, VExpandTransition: slot, VCol: slot, VRow: slot,
        VTooltip: slot, VSlider: true, VSelect: true, VSwitch: true, VSnackbar: slot,
        VDialog: hiddenModal, VOverlay: hiddenModal,
        RouterLink: false, RouterView: false,
        AddChannelModal: true, EditChannelModal: true, UpdateDialog: true,
        UserGuideDialog: true, CapabilityTestDialog: true, Logo: true,
      },
    },
  })
  try {
    const destinations = [
      '/channels/messages', '/channels/images', '/channels/vectors', '/conversations', '/health',
      '/subscriptions', '/cockpit', '/autopilot', '/console', '/cost-report',
    ]
    expect(wrapper.findAll('a.api-type-text').map(link => link.attributes('href'))).toEqual(destinations)
    for (const path of [...destinations.slice(1), destinations[0]!]) {
      const link = wrapper.get(`a.api-type-text[href="${path}"]`)
      await link.trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.path).toBe(path)
      expect(wrapper.get('[data-page]').text()).toBe(path)
      expect(link.classes()).toContain('active')
      expect(wrapper.findAll('a.api-type-text.active')).toHaveLength(1)
    }
  } finally {
    wrapper.unmount()
  }
})
