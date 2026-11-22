// @vitest-environment jsdom
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import ClaudeDesktopModels from './ClaudeDesktopModels.vue'
import type { ChannelProtocolRoute } from '../../services/api'

vi.mock('../../i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const select = defineComponent({
  props: ['modelValue', 'items', 'disabled'], emits: ['update:modelValue'],
  template: `<select :value="modelValue" :disabled="disabled" @change="$emit('update:modelValue', $event.target.value)"><option v-for="item in items" :key="item.value" :value="item.value">{{ item.title }}</option></select>`,
})
const input = defineComponent({
  props: ['modelValue', 'disabled'], emits: ['update:modelValue'],
  template: `<input :value="modelValue" :disabled="disabled" @input="$emit('update:modelValue', $event.target.value)" />`,
})
const button = defineComponent({ props: ['disabled'], template: '<button :disabled="disabled"><slot /></button>' })
const routes: ChannelProtocolRoute[] = [
  { kind: 'messages', index: 0, channelUid: 'deepseek', name: 'DeepSeek', serviceType: 'claude', discoveredModels: ['deepseek-v4-pro', 'deepseek-flash'] },
  { kind: 'messages', index: 1, channelUid: 'other', name: 'Other', serviceType: 'claude', claudeDesktopModelTiers: { custom: 'opus' } },
  { kind: 'chat', index: 0, channelUid: 'chat', name: 'Chat', serviceType: 'openai' },
]
const setup = (value = routes, disabled = false) => mount(ClaudeDesktopModels, {
  props: { modelValue: value, disabled }, global: { stubs: { VSelect: select, VTextField: input, VBtn: button } },
})

describe('ClaudeDesktopModels', () => {
  it('offers only None and supported tiers for discovered model IDs', () => {
    const wrapper = setup()
    expect(wrapper.get('[data-model-id="deepseek-v4-pro"]').findAll('option').map(option => option.attributes('value')))
      .toEqual(['', 'sonnet', 'opus', 'haiku', 'fable', 'mythos'])
    expect(wrapper.find('[data-route-uid="chat"]').exists()).toBe(false)
  })
  it('stages independent selections, preserves other routes, and restores saved values', async () => {
    const wrapper = setup()
    await wrapper.get('[data-model-id="deepseek-v4-pro"]').setValue('sonnet')
    const edited = wrapper.emitted('update:modelValue')![0]![0] as ChannelProtocolRoute[]
    expect(edited[0]!.claudeDesktopModelTiers).toEqual({ 'deepseek-v4-pro': 'sonnet' })
    expect(edited[1]).toEqual(routes[1])
    expect(routes[0]!.claudeDesktopModelTiers).toBeUndefined()
    await wrapper.setProps({ modelValue: edited })
    await wrapper.get('[data-model-id="deepseek-flash"]').setValue('haiku')
    const saved = wrapper.emitted('update:modelValue')![1]![0] as ChannelProtocolRoute[]
    const reopened = setup(saved)
    expect((reopened.get('[data-model-id="deepseek-v4-pro"]').element as HTMLSelectElement).value).toBe('sonnet')
    expect((reopened.get('[data-model-id="deepseek-flash"]').element as HTMLSelectElement).value).toBe('haiku')
    await reopened.get('[data-model-id="deepseek-v4-pro"]').setValue('')
    expect((reopened.emitted('update:modelValue')![0]![0] as ChannelProtocolRoute[])[0]!.claudeDesktopModelTiers)
      .toEqual({ 'deepseek-v4-pro': '', 'deepseek-flash': 'haiku' })
  })
  it('supports an explicit model ID when discovery has no inventory', async () => {
    const wrapper = setup([{ ...routes[0]!, discoveredModels: [] }])
    await wrapper.get('input').setValue('custom-model')
    await wrapper.get('button').trigger('click')
    await wrapper.get('[data-model-id="custom-model"]').setValue('opus')
    expect((wrapper.emitted('update:modelValue')![0]![0] as ChannelProtocolRoute[])[0]!.claudeDesktopModelTiers).toEqual({ 'custom-model': 'opus' })
  })
  it('does not change state when disabled or when the route lacks a stable UID', async () => {
    const wrapper = setup(routes, true)
    wrapper.findComponent(select).vm.$emit('update:modelValue', 'sonnet')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    const unresolved = setup([{ ...routes[0]!, channelUid: undefined }])
    unresolved.findComponent(select).vm.$emit('update:modelValue', 'sonnet')
    expect(unresolved.emitted('update:modelValue')).toBeUndefined()
  })
})
