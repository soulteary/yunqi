// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RequestPreview from './RequestPreview.vue'
const mocks = vi.hoisted(() => ({ previewChannelRequest: vi.fn() }))
vi.mock('../services/api', () => ({ api: mocks }))
vi.mock('../i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const pass = defineComponent({ template: '<div><slot /></div>' })
function render() {
 return mount(RequestPreview, { props: { channelUid: 'ch_test', channelType: 'chat', draft: { serviceType: 'openai', fastMode: false, apiKeys: ['private'] } }, global: { stubs: { VExpansionPanels: pass, VExpansionPanel: pass, VExpansionPanelText: pass, VAlert: pass, VSelect: pass, VTextField: pass, VTextarea: pass, VList: pass, VListItem: pass, VBtn: { template: '<button><slot /></button>' } } } })
}
const result = { mode: 'native_with_compatibility', sourceProtocol: 'chat', targetProtocol: 'chat', method: 'POST', url: 'https://example.test/v1/chat/completions', headers: { Authorization: '[REDACTED]' }, body: { model: 'sample' }, effects: [], boundaries: [], bodyOmitted: false }
describe('request preview', () => {
 beforeEach(() => { vi.clearAllMocks() })
 it('sends only explicit sample/draft options after click and clears stale result on edits', async () => {
  mocks.previewChannelRequest.mockResolvedValue(result)
  const wrapper = render()
  expect(mocks.previewChannelRequest).not.toHaveBeenCalled()
  await wrapper.get('button').trigger('click'); await flushPromises()
  expect(mocks.previewChannelRequest).toHaveBeenCalledWith('ch_test', expect.objectContaining({ protocol: 'chat', draft: { serviceType: 'openai', fastMode: false } }))
  expect(wrapper.find('pre').text()).toContain('[REDACTED]')
  await wrapper.setProps({ draft: { serviceType: 'claude' } })
  expect(wrapper.find('pre').exists()).toBe(false)
 })
 it('discards a delayed response for a previous configuration', async () => {
  let resolve!: (value: typeof result) => void
  mocks.previewChannelRequest.mockImplementation(() => new Promise(r => { resolve = r }))
  const wrapper = render(); await wrapper.get('button').trigger('click')
  await wrapper.setProps({ channelUid: 'ch_other' })
  resolve(result); await flushPromises()
  expect(wrapper.find('pre').exists()).toBe(false)
 })
})
