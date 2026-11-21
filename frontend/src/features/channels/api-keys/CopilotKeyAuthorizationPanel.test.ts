// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CopilotKeyAuthorizationPanel from './CopilotKeyAuthorizationPanel.vue'

const apiMocks = vi.hoisted(() => ({
  requestCopilotDeviceCode: vi.fn(),
  pollCopilotAccessToken: vi.fn(),
  diagnoseCopilotChannel: vi.fn(),
}))
vi.mock('../../../services/api', () => ({
  ApiService: class {
    requestCopilotDeviceCode = apiMocks.requestCopilotDeviceCode
    pollCopilotAccessToken = apiMocks.pollCopilotAccessToken
    diagnoseCopilotChannel = apiMocks.diagnoseCopilotChannel
  },
}))
vi.mock('../../../i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const passthrough = defineComponent({ template: '<div><slot /></div>' })
const button = defineComponent({ template: '<button type="button"><slot /></button>' })
const input = defineComponent({
  props: ['modelValue'],
  emits: ['update:modelValue'],
  template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
})
const wrappers: Array<ReturnType<typeof mount>> = []
const mountPanel = (props: Record<string, unknown> = {}) => {
  const wrapper = mount(CopilotKeyAuthorizationPanel, {
    props: { apiKeys: ['existing-key'], serviceType: 'copilot', isEditing: true, channelId: 3, ...props },
    global: { stubs: { VAlert: passthrough, VBtn: button, VTextField: input, VIcon: passthrough, VChip: passthrough } },
  })
  wrappers.push(wrapper)
  return wrapper
}
const click = async (wrapper: ReturnType<typeof mountPanel>, label: string) => {
  const target = wrapper.findAll('button').find(item => item.text().endsWith(label))
  expect(target, label).toBeDefined()
  await target!.trigger('click')
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.spyOn(window, 'open').mockReturnValue(null)
  apiMocks.requestCopilotDeviceCode.mockResolvedValue({ deviceCode: 'device-code', userCode: 'USER-CODE', verificationUri: 'https://github.com/login/device', interval: 1 })
  apiMocks.pollCopilotAccessToken.mockResolvedValue({ accessToken: 'oauth-key' })
  apiMocks.diagnoseCopilotChannel.mockResolvedValue({ githubUser: { login: 'example' }, modelsStatus: 200 })
})
afterEach(() => {
  for (const wrapper of wrappers.splice(0)) wrapper.unmount()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('Copilot Key authorization', () => {
  it('preserves proxy changes and appends an authorized token without replacing existing keys', async () => {
    const wrapper = mountPanel({ proxyUrl: '  http://localhost:8080  ' })
    await wrapper.get('input').setValue('http://localhost:9090')
    expect(wrapper.emitted('update:proxyUrl')).toEqual([['http://localhost:9090']])
    await click(wrapper, 'copilotOAuth.button')
    expect(apiMocks.requestCopilotDeviceCode).toHaveBeenCalledWith('http://localhost:8080')
    expect(apiMocks.pollCopilotAccessToken).toHaveBeenCalledWith('device-code', 'http://localhost:8080')
    expect(wrapper.emitted('update:apiKeys')).toEqual([[['existing-key', 'oauth-key']]])
    expect(wrapper.text()).toContain('copilotOAuth.success')
    expect(window.open).toHaveBeenCalledWith('https://github.com/login/device', '_blank', 'noopener,noreferrer')
  })

  it('does not append a duplicate authorized token and diagnoses using the latest first key', async () => {
    const wrapper = mountPanel({ apiKeys: ['oauth-key'] })
    await click(wrapper, 'copilotOAuth.button')
    expect(wrapper.emitted('update:apiKeys')).toBeUndefined()
    await wrapper.setProps({ apiKeys: ['new-first-key', 'oauth-key'], channelId: 9 })
    await click(wrapper, 'copilotDiagnose.button')
    expect(apiMocks.diagnoseCopilotChannel).toHaveBeenCalledWith(9, 'new-first-key')
    expect(wrapper.text()).toContain('example')
  })

  it.each(['cancel', 'unmount'] as const)('clears pending authorization polling on %s', async action => {
    vi.useFakeTimers()
    apiMocks.pollCopilotAccessToken.mockResolvedValue({ error: 'authorization_pending' })
    const wrapper = mountPanel()
    await click(wrapper, 'copilotOAuth.button')
    expect(apiMocks.pollCopilotAccessToken).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('copilotOAuth.waiting')
    await vi.advanceTimersByTimeAsync(4999)
    expect(apiMocks.pollCopilotAccessToken).toHaveBeenCalledTimes(1)
    if (action === 'cancel') await click(wrapper, 'copilotOAuth.cancel')
    else wrapper.unmount()
    await vi.advanceTimersByTimeAsync(10000)
    expect(apiMocks.pollCopilotAccessToken).toHaveBeenCalledTimes(1)
  })

  it('shows an expired token error and permits a new authorization attempt', async () => {
    apiMocks.pollCopilotAccessToken.mockResolvedValueOnce({ error: 'expired_token' })
    const wrapper = mountPanel()
    await click(wrapper, 'copilotOAuth.button')
    expect(wrapper.text()).toContain('copilotOAuth.expired')
    expect(wrapper.emitted('update:apiKeys')).toBeUndefined()
    await click(wrapper, 'copilotOAuth.retry')
    expect(apiMocks.requestCopilotDeviceCode).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('update:apiKeys')).toEqual([[['existing-key', 'oauth-key']]])
  })
})
