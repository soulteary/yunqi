// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AddChannelModal from './AddChannelModal.vue'
import { autoAddChannel, discoverFast } from '../services/autopilot-api'

vi.mock('../i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vuetify', () => ({ useTheme: () => ({ global: { current: { value: { dark: false } } } }) }))
vi.mock('../stores/auth', () => ({ useAuthStore: () => ({ isAuthenticated: false, apiKey: '' }) }))
vi.mock('../stores/channel', () => ({ useChannelStore: () => Object.fromEntries(
  ['channelsData', 'chatChannelsData', 'responsesChannelsData', 'geminiChannelsData', 'imagesChannelsData', 'vectorsChannelsData']
    .map(name => [name, { channels: [] }]),
) }))
vi.mock('../stores/preferences', () => ({ usePreferencesStore: () => ({ newChannelPlacement: 'bottom' }) }))
vi.mock('@/composables/useDialogHotkeys', () => ({ useDialogHotkeys: vi.fn() }))
vi.mock('../services/autopilot-api', () => ({
  autoAddChannel: vi.fn(), discoverFast: vi.fn(), preloadProviderTemplates: vi.fn(),
  extractAutoAddErrorMessage: (error: Error) => error.message,
}))

const textarea = defineComponent({
  props: ['modelValue', 'label', 'disabled'], emits: ['update:modelValue', 'input'],
  template: '<label>{{label}}<textarea :value="modelValue" :disabled="disabled" @input="$emit(\'update:modelValue\', $event.target.value); $emit(\'input\')" /></label>',
})
const render = () => mount(AddChannelModal, { props: { show: true, channelType: 'chat' }, global: { stubs: {
  ...Object.fromEntries(['VDialog', 'VCard', 'VCardTitle', 'VCardText', 'VCardActions', 'VAvatar', 'VIcon', 'VAlert', 'VBtnToggle'].map(name => [name, { template: '<div><slot /></div>' }])),
  VTextarea: textarea, VTextField: true, VSwitch: true, VSpacer: true, QuickAddChannelForm: true,
  VBtn: { props: ['disabled'], template: '<button :disabled="disabled"><slot /></button>' },
} } })
let view: ReturnType<typeof render> | undefined
afterEach(() => view?.unmount())
beforeEach(() => {
  vi.mocked(discoverFast).mockReset().mockResolvedValue({ primaryKind: 'chat', routes: [] })
  vi.mocked(autoAddChannel).mockReset().mockResolvedValue({
    index: 0, channels: [], accountUid: 'test-account', channelUid: 'test-channel', discoveryStarted: true,
  })
})
const createButton = () => view!.findAll('button').find(button => button.text().includes('addChannel.createChannel'))!
const submit = async () => { await createButton().trigger('click'); await flushPromises() }

describe('standard channel import with opaque credentials', () => {
  it('submits manual keys verbatim through discovery and creation, overriding detected keys', async () => {
    view = render()
    await view.get('.quick-input-textarea textarea').setValue('https://example.com sk-detected1234567890')
    const opaque = 'a'.repeat(32) + '.short!?/+=:secret'
    await view.get('[data-test="manual-api-keys"] textarea').setValue('  ' + opaque + '\r\n' + opaque + '\ncustom:second-key==')
    await submit()
    expect(discoverFast).toHaveBeenCalledWith('chat', ['https://example.com'], [opaque, 'custom:second-key=='], expect.any(Object))
    expect(autoAddChannel).toHaveBeenCalledWith('chat', expect.objectContaining({ apiKeys: [opaque, 'custom:second-key=='] }))
  })
  it('restores automatic recognition when manual input is cleared', async () => {
    view = render()
    await view.get('.quick-input-textarea textarea').setValue('https://example.com sk-detected1234567890')
    await view.get('[data-test="manual-api-keys"] textarea').setValue('custom-key')
    await view.get('[data-test="manual-api-keys"] textarea').setValue('  \n')
    await submit()
    expect(discoverFast).toHaveBeenCalledWith('chat', ['https://example.com'], ['sk-detected1234567890'], expect.any(Object))
  })
  it('requires an address and a key and discards manual credentials after reopening', async () => {
    view = render()
    await view.get('.quick-input-textarea textarea').setValue('https://example.com')
    expect(createButton().attributes('disabled')).toBeDefined()
    await view.get('[data-test="manual-api-keys"] textarea').setValue('opaque-secret')
    expect(createButton().attributes('disabled')).toBeUndefined()
    await view.setProps({ show: false })
    await view.setProps({ show: true })
    expect((view.get('[data-test="manual-api-keys"] textarea').element as HTMLTextAreaElement).value).toBe('')
    expect(createButton().attributes('disabled')).toBeDefined()
    expect(discoverFast).not.toHaveBeenCalled()
  })
})
