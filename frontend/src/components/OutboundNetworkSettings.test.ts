// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import OutboundNetworkSettings from './OutboundNetworkSettings.vue'
const mocks = vi.hoisted(() => ({ getNetworkInterfaces: vi.fn(), diagnoseNetwork: vi.fn() }))
vi.mock('../services/api', () => ({ api: mocks }))
vi.mock('../i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const select = defineComponent({ props: ['modelValue', 'items', 'label', 'disabled'], emits: ['update:modelValue'], template: '<div />' })
const pass = defineComponent({ template: '<div><slot /></div>' })
function mountSettings(props: Record<string, unknown> = {}) {
  return mount(OutboundNetworkSettings, { props, global: { stubs: { VSelect: select, VTextarea: select, VBtn: { template: '<button><slot /></button>' }, VAlert: pass } } })
}
describe('出口设置', () => {
  it('强制直连切换保留网卡和 DNS，不改写代理字段', () => {
    const original = { proxyMode: 'legacy' as const, interfaceName: 'en0', dns: { mode: 'bound' as const, servers: ['192.0.2.53:53'] } }
    const wrapper = mountSettings({ modelValue: original, proxyUrl: 'http://localhost:7890' })
    wrapper.findComponent<typeof select>('[data-testid="proxy-mode"]').vm.$emit('update:modelValue', 'direct')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ ...original, proxyMode: 'direct' }])
    expect(original.proxyMode).toBe('legacy')
    expect(wrapper.emitted('update:proxyUrl')).toBeUndefined()
  })
  it('选定网卡要求 bound DNS，保留不可用接口', () => {
    const wrapper = mountSettings({ modelValue: { interfaceName: 'missing0', dns: { mode: 'bound', servers: ['127.0.0.1:53'] } } })
    const iface = wrapper.findComponent<typeof select>('[data-testid="egress-interface"]')
    expect(iface.props('items')).toContainEqual({ title: 'missing0 · network.unavailable', value: 'missing0' })
    iface.vm.$emit('update:modelValue', 'en1')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([{ interfaceName: 'en1', dns: { mode: 'bound', servers: ['127.0.0.1:53'] } }])
  })
  it('只在显式点击后诊断，发送出口设置但不发送 Key', async () => {
    mocks.diagnoseNetwork.mockResolvedValue({ results: [{ branch: 'direct', stage: 'complete', success: true, bindingApplied: false }] })
    const wrapper = mountSettings({ baseUrl: 'https://example.com', proxyUrl: 'http://localhost:7890', modelValue: { proxyMode: 'direct' } })
    expect(mocks.diagnoseNetwork).not.toHaveBeenCalled()
    await wrapper.findAll('button')[1]!.trigger('click')
    await flushPromises()
    expect(mocks.diagnoseNetwork).toHaveBeenCalledWith(expect.objectContaining({ outboundNetwork: { proxyMode: 'direct' }, baseUrl: 'https://example.com' }))
    expect(mocks.diagnoseNetwork.mock.calls[0]![0]).not.toHaveProperty('apiKeys')
  })
})
