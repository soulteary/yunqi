// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import { api } from '@/services/api'
import type { ClientMappingState } from '@/services/types/client-model-mappings'
import ClientModelMappings from './ClientModelMappings.vue'

vi.mock('@/i18n', () => ({ useI18n: () => ({ locale: ref('zh-CN') }) }))
vi.mock('@/services/api', () => ({ api: { getClientModelMappings: vi.fn(), setClientModelMappings: vi.fn() } }))
const state: ClientMappingState = {
  mappings: { 'claude-desktop': { opus: 'deepseek-v4-pro' }, codex: { default: 'codex-model' } },
  targets: [
    { model: 'deepseek-v4-pro', kind: 'messages', channels: ['DeepSeek'] },
    { model: 'deepseek-flash', kind: 'messages', channels: ['DeepSeek'] },
    { model: 'codex-model', kind: 'responses', channels: ['Responses'] },
    { model: 'chat-model', kind: 'chat', channels: ['Chat'] }
  ]
}
const render = () => mount(ClientModelMappings, {
  global: { stubs: {
    VCard: { template: '<section><slot /></section>' },
    VAlert: { template: '<div><slot /></div>' },
    VBtn: { props: ['disabled', 'loading'], template: '<button :disabled="disabled || loading"><slot /></button>' },
    VSelect: {
      props: ['modelValue', 'disabled', 'items', 'label', 'hint'], emits: ['update:modelValue'],
      template: `<label>{{label}}<select :disabled="disabled" :value="modelValue" @change="$emit('update:modelValue', $event.target.value)"><option v-for="item in items" :key="item.value" :value="item.value">{{item.title}}</option></select><small>{{hint}}</small></label>`
    }
  } }
})
describe('client model mapping editor', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    useAuthStore().apiKey = 'test-admin'
    vi.mocked(api.getClientModelMappings).mockReset().mockImplementation(async () => structuredClone(state))
    vi.mocked(api.setClientModelMappings).mockReset().mockImplementation(async mappings => ({ mappings: JSON.parse(JSON.stringify(mappings)), targets: state.targets }))
  })
  it('uses five fixed client tiers and lets all tiers share a channel model, saving only explicitly', async () => {
    const view = render()
    await flushPromises()
    expect(view.findAll('[data-slot]').map(row => row.attributes('data-slot'))).toEqual(['haiku', 'sonnet', 'opus', 'fable', 'mythos'])
    for (const row of view.findAll('[data-slot]')) {
      expect(row.text()).toContain('deepseek-flash')
      expect(row.text()).not.toContain('codex-model')
      await row.get('select').setValue('deepseek-v4-pro')
    }
    expect(api.setClientModelMappings).not.toHaveBeenCalled()
    await view.get('[data-test="mapping-save"]').trigger('click')
    await flushPromises()
    expect(api.setClientModelMappings).toHaveBeenCalledWith({
      'claude-desktop': { haiku: 'deepseek-v4-pro', sonnet: 'deepseek-v4-pro', opus: 'deepseek-v4-pro', fable: 'deepseek-v4-pro', mythos: 'deepseek-v4-pro' },
      codex: { default: 'codex-model' }
    })
    expect(view.text()).toContain('映射已保存')
    view.unmount()
  })
  it('switches clients without losing drafts and filters by the client protocol', async () => {
    const view = render(); await flushPromises()
    await view.get('[data-slot="haiku"] select').setValue('deepseek-flash')
    await view.get('[data-test="mapping-client"] select').setValue('codex')
    expect(view.findAll('[data-slot]').length).toBe(1)
    expect(view.get('[data-slot="default"]').text()).toContain('codex-model')
    expect(view.get('[data-slot="default"]').text()).not.toContain('deepseek-flash')
    await view.get('[data-test="mapping-client"] select').setValue('claude-desktop')
    expect((view.get('[data-slot="haiku"] select').element as HTMLSelectElement).value).toBe('deepseek-flash')
    await view.get('[data-slot="opus"] select').setValue('')
    await view.get('[data-test="mapping-save"]').trigger('click'); await flushPromises()
    expect(api.setClientModelMappings).toHaveBeenCalledWith({ 'claude-desktop': { haiku: 'deepseek-flash' }, codex: { default: 'codex-model' } })
    view.unmount()
  })
  it('keeps unavailable saved selections visible and preserves drafts after save failures', async () => {
    vi.mocked(api.getClientModelMappings).mockResolvedValue({ ...state, targets: [] })
    vi.mocked(api.setClientModelMappings).mockRejectedValue(new Error('save failed'))
    const view = render(); await flushPromises()
    expect(view.get('[data-slot="opus"]').text()).toContain('当前不可用')
    await view.get('[data-slot="opus"] select').setValue('')
    await view.get('[data-test="mapping-save"]').trigger('click'); await flushPromises()
    expect(view.text()).toContain('save failed')
    expect((view.get('[data-slot="opus"] select').element as HTMLSelectElement).value).toBe('')
    view.unmount()
  })
  it('blocks saving after a load failure and discards results from a previous login', async () => {
    let resolve!: (value: ClientMappingState) => void
    vi.mocked(api.getClientModelMappings).mockReturnValue(new Promise(done => { resolve = done }))
    const view = render()
    useAuthStore().apiKey = null; await flushPromises()
    resolve(state); await flushPromises()
    expect(view.text()).not.toContain('deepseek-v4-pro')
    expect(view.get('[data-test="mapping-save"]').attributes('disabled')).toBeDefined()
    view.unmount()
    vi.mocked(api.getClientModelMappings).mockRejectedValue(new Error('load failed'))
    useAuthStore().apiKey = 'next-admin'
    const failed = render(); await flushPromises()
    expect(failed.text()).toContain('load failed')
    expect(failed.get('[data-test="mapping-save"]').attributes('disabled')).toBeDefined()
    failed.unmount()
  })
})
