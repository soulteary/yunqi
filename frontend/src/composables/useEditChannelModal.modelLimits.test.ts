// @vitest-environment jsdom
import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, reactive } from 'vue'
import { expect, it, vi } from 'vitest'
import { useEditChannelModal } from './useEditChannelModal'
import type { Channel } from '../services/api'

vi.mock('../i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vuetify', () => ({ useTheme: () => ({ global: { current: { value: { dark: false } } } }) }))
vi.mock('../stores/channel', () => ({ useChannelStore: () => ({}) }))
vi.mock('../stores/dialog', () => ({ useDialogStore: () => ({}) }))

it('白名单修改暂存到保存，取消后重新打开恢复原值', async () => {
  const channel = {
    index: 0, channelUid: 'route', name: 'test', baseUrl: 'https://example.com',
    apiKeys: ['test'], serviceType: 'responses', supportedModels: ['gpt*'],
  } as Channel
  const props = reactive({ show: false, channelType: 'responses' as const, channel })
  const emit = vi.fn()
  let editor!: ReturnType<typeof useEditChannelModal>
  const wrapper = mount(defineComponent({ setup() {
    editor = useEditChannelModal(props, emit)
    return () => h('div')
  } }))
  try {
    props.show = true
    await nextTick()
    editor.protocolModelLimits.value[0]!.supportedModels = []
    expect(channel.supportedModels).toEqual(['gpt*'])
    editor.handleCancel()
    props.show = false
    await nextTick()
    props.show = true
    await nextTick()
    expect(editor.protocolModelLimits.value[0]!.supportedModels).toEqual(['gpt*'])
    editor.protocolModelLimits.value[0]!.supportedModels = []
    editor.formRef.value = { validate: async () => ({ valid: true }) }
    await editor.handleSubmit()
    const save = emit.mock.calls.find(call => call[0] === 'save')
    expect(save?.[1].protocolRoutes).toEqual([expect.objectContaining({ channelUid: 'route', supportedModels: [] })])
    expect(channel.supportedModels).toEqual(['gpt*'])
  } finally { wrapper.unmount() }
})
