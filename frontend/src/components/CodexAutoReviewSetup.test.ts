// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'
import { api } from '@/services/api'
import CodexAutoReviewSetup from './CodexAutoReviewSetup.vue'

vi.mock('@/i18n', () => ({ useI18n: () => ({ locale: ref('zh-CN') }) }))
vi.mock('@/services/api', () => ({ api: { getCodexAutoReviewMapping: vi.fn(), setCodexAutoReviewMapping: vi.fn() } }))
const render = () =>
  mount(CodexAutoReviewSetup, {
    props: { model: 'working-model' },
    global: {
      stubs: {
        VCard: { template: '<section><slot /></section>' },
        VAlert: { template: '<div><slot /></div>' },
        VBtn: { props: ['disabled'], template: '<button :disabled="disabled"><slot /></button>' },
        VTextField: {
          props: ['modelValue', 'disabled'],
          emits: ['update:modelValue'],
          template:
            '<input :disabled="disabled" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)">'
        }
      }
    }
  })
describe('Codex auto-review shortcut', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    useAuthStore().apiKey = 'admin-test'
    vi.mocked(api.getCodexAutoReviewMapping).mockReset().mockResolvedValue({ enabled: false, targetModel: '' })
    vi.mocked(api.setCodexAutoReviewMapping)
      .mockReset()
      .mockImplementation(async mapping => mapping)
  })
  it('opens read-only, prefills the current model, and persists only on explicit save', async () => {
    const view = render()
    expect(api.getCodexAutoReviewMapping).not.toHaveBeenCalled()
    await view.get('[data-test="review-open"]').trigger('click')
    await flushPromises()
    expect((view.get('input').element as HTMLInputElement).value).toBe('working-model')
    expect(api.setCodexAutoReviewMapping).not.toHaveBeenCalled()
    await view.get('[data-test="review-save"]').trigger('click')
    await flushPromises()
    expect(api.setCodexAutoReviewMapping).toHaveBeenLastCalledWith({ enabled: true, targetModel: 'working-model' })
    expect(view.text()).toContain('映射已启用并保存')
    await view.get('[data-test="review-disable"]').trigger('click')
    await flushPromises()
    expect(api.setCodexAutoReviewMapping).toHaveBeenLastCalledWith({ enabled: false, targetModel: 'working-model' })
    expect(view.text()).toContain('映射已停用')
    view.unmount()
  })
  it('loads the saved mapping, rejects self-mapping, and keeps the old mapping on failure', async () => {
    vi.mocked(api.getCodexAutoReviewMapping).mockResolvedValue({ enabled: true, targetModel: 'saved-model' })
    const view = render()
    await view.get('[data-test="review-open"]').trigger('click')
    await flushPromises()
    expect((view.get('input').element as HTMLInputElement).value).toBe('saved-model')
    await view.get('input').setValue('codex-auto-review')
    expect(view.get('[data-test="review-save"]').attributes('disabled')).toBeDefined()
    await view.get('input').setValue('new-model')
    vi.mocked(api.setCodexAutoReviewMapping).mockRejectedValueOnce(new Error('save failed'))
    await view.get('[data-test="review-save"]').trigger('click')
    await flushPromises()
    expect(view.text()).toContain('save failed')
    expect(view.text()).toContain('当前已启用：saved-model')
    view.unmount()
  })
  it('does not offer saving after a failed load and can retry', async () => {
    vi.mocked(api.getCodexAutoReviewMapping).mockRejectedValueOnce(new Error('load failed'))
    const view = render()
    await view.get('[data-test="review-open"]').trigger('click')
    await flushPromises()
    expect(view.text()).toContain('load failed')
    expect(view.find('[data-test="review-save"]').exists()).toBe(false)
    await view.get('[data-test="review-open"]').trigger('click')
    await flushPromises()
    expect(view.find('[data-test="review-save"]').exists()).toBe(true)
    view.unmount()
  })
  it('discards a pending management response after logout', async () => {
    let resolve!: (mapping: { enabled: boolean; targetModel: string }) => void
    vi.mocked(api.getCodexAutoReviewMapping).mockReturnValue(
      new Promise(done => {
        resolve = done
      })
    )
    const view = render()
    await view.get('[data-test="review-open"]').trigger('click')
    useAuthStore().apiKey = null
    await flushPromises()
    resolve({ enabled: true, targetModel: 'private-model' })
    await flushPromises()
    expect(view.find('input').exists()).toBe(false)
    expect(view.text()).not.toContain('private-model')
    expect(api.setCodexAutoReviewMapping).not.toHaveBeenCalled()
    view.unmount()
  })
})
