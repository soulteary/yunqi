// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import { api } from '@/services/api'
import CostReportView from './CostReportView.vue'

vi.mock('@/i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/services/api', () => ({ api: { getCostReport: vi.fn() } }))

afterEach(() => { vi.unstubAllGlobals() })

it('downloads a CSV with escaped model names and safe spreadsheet cells from the real export button', async () => {
  const payloads: string[] = []
  vi.stubGlobal('Blob', class {
    constructor(parts: string[]) { payloads.push(parts.join('')) }
  })
  const createObjectURL = vi.fn(() => 'blob:cost-report')
  const revokeObjectURL = vi.fn()
  vi.stubGlobal('URL', { createObjectURL, revokeObjectURL })
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  vi.mocked(api.getCostReport).mockResolvedValue({
    groupBy: 'user', duration: '7d', apiType: 'messages', rows: [{
      groupKey: 'company,model\n"v2"', totalRequests: 1, successCount: 1,
      inputTokens: 12, outputTokens: 3, cacheCreationTokens: 0, cacheReadTokens: 0,
      listCostUSD: 0.01, effectiveCostUSD: 0.01, unpricedModels: ['=1+1'],
    }],
  })
  const names = ['VCard', 'VCardText', 'VRow', 'VCol', 'VTable', 'VTooltip', 'VIcon']
  const view = mount(CostReportView, { global: { stubs: {
    ...Object.fromEntries(names.map(name => [name, { template: '<div><slot /></div>' }])),
    VBtn: { props: ['disabled'], template: '<button :disabled="disabled"><slot /></button>' },
    VSelect: true, VChip: { template: '<span><slot /></span>' },
  } } })
  try {
    await flushPromises()
    await view.findAll('button').find(button => button.text() === 'costReport.exportCsv')!.trigger('click')
    expect(click).toHaveBeenCalledOnce()
    expect(payloads).toHaveLength(1)
    expect(payloads[0]).toContain('"company,model\n""v2"""')
    expect(payloads[0]).toContain("'=1+1")
    expect(payloads[0]).toContain('12,3,0,0')
    expect(createObjectURL).toHaveBeenCalledOnce()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:cost-report')
  } finally {
    view.unmount()
    click.mockRestore()
  }
})
