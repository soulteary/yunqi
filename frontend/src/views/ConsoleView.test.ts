// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import ConsoleView from './ConsoleView.vue'
import { api } from '@/services/api'
vi.mock('@/i18n', () => ({ useI18n: () => ({ locale: ref('zh-CN') }) }))
vi.mock('@/services/api', () => ({ api: { getConsoleStatus: vi.fn() } }))
const status = {
  version: { version: 'v3.0.0', buildTime: 'test-build', gitCommit: 'test-commit' },
  uptimeSeconds: 180,
  mode: 'production',
  settings: { port: 3688, bindHost: '127.0.0.1', webUI: true },
  channels: { messages: 1, chat: 2, responses: 3, gemini: 4, images: 5, vectors: 6 }
}
function render() {
  const names = ['VCard', 'VAlert', 'VRow', 'VCol', 'VSelect', 'VTextField', 'VDivider']
  const stubs = Object.fromEntries(names.map(name => [name, { template: '<div><slot /></div>' }]))
  return mount(ConsoleView, {
    global: {
      stubs: {
        ...stubs,
        VBtn: { template: '<button><slot /></button>' },
        VSelect: {
          props: ['modelValue', 'items'],
          emits: ['update:modelValue'],
          template:
            '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="item in items" :key="item.value" :value="item.value">{{item.title}}</option></select>'
        },
        VTextField: {
          props: ['modelValue', 'label', 'hint'],
          emits: ['update:modelValue'],
          template:
            '<label>{{label}}<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)"><small>{{hint}}</small></label>'
        }
      }
    }
  })
}
describe('Web service console', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    useAuthStore().apiKey = 'test-admin'
    vi.mocked(api.getConsoleStatus).mockReset()
  })
  it('exports a user-selected Codex context window, clears it, and blocks invalid values', async () => {
    vi.mocked(api.getConsoleStatus).mockResolvedValue(status)
    const view = render()
    await flushPromises()
    await view.get('[data-test="client-model"] input').setValue('gpt-5.2')
    await view.get('select').setValue('codex')
    expect(view.get('pre').text()).not.toContain('model_context_window')
    expect(view.text()).toContain('不能扩展上游模型的实际容量')
    const context = view.get('[data-test="codex-context-window"] input')
    await context.setValue('1000000')
    expect(view.get('pre').text()).toContain('model_context_window = 1000000')
    await context.setValue('0')
    expect(view.find('pre').exists()).toBe(false)
    expect(view.text()).toContain('须为正整数')
    await view.get('select').setValue('claude')
    expect(view.get('pre').text()).not.toContain('model_context_window')
    await view.get('select').setValue('codex')
    await view.get('[data-test="codex-context-window"] input').setValue('')
    expect(view.get('pre').text()).not.toContain('model_context_window')
    view.unmount()
  })
  it('shows server runtime information and all six channel counts', async () => {
    vi.mocked(api.getConsoleStatus).mockResolvedValue(status)
    const view = render()
    await flushPromises()
    expect(view.text()).toContain('v3.0.0')
    expect(view.text()).toContain('127.0.0.1:3688')
    expect(view.text()).toContain('vectors: 6')
    expect(view.text()).toContain('PROXY_ACCESS_KEY')
    view.unmount()
  })
  it('displays API failures and allows retrying', async () => {
    vi.mocked(api.getConsoleStatus).mockRejectedValueOnce(new Error('Unauthorized')).mockResolvedValueOnce(status)
    const view = render()
    await flushPromises()
    expect(view.text()).toContain('Unauthorized')
    await view.find('button').trigger('click')
    await flushPromises()
    expect(view.text()).not.toContain('Unauthorized')
    expect(view.text()).toContain('test-commit')
    view.unmount()
  })
  it('waits for authentication on a direct console visit and reloads after login', async () => {
    const auth = useAuthStore()
    auth.apiKey = null
    vi.mocked(api.getConsoleStatus).mockResolvedValue(status)
    const view = render()
    await flushPromises()
    expect(api.getConsoleStatus).not.toHaveBeenCalled()
    auth.apiKey = 'test-admin'
    await flushPromises()
    expect(view.text()).toContain('test-commit')
    auth.apiKey = null
    await flushPromises()
    expect(view.text()).not.toContain('test-commit')
    view.unmount()
  })
  it('discards an earlier response after logout', async () => {
    let resolve!: (value: typeof status) => void
    vi.mocked(api.getConsoleStatus).mockReturnValue(
      new Promise(done => {
        resolve = done
      })
    )
    const view = render()
    useAuthStore().apiKey = null
    await flushPromises()
    resolve(status)
    await flushPromises()
    expect(view.text()).not.toContain('test-commit')
    view.unmount()
  })
})
