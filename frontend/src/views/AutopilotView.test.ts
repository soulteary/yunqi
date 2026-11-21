// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AutopilotView from './AutopilotView.vue'
import AutopilotTraceTable from '@/components/AutopilotTraceTable.vue'
import { api } from '@/services/api'
import type { AutopilotTraceListResponse, AutopilotTraceStats, SmartRoutingConfig } from '@/services/api-types'
vi.mock('@/i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/services/api', () => ({
  api: {
    getSmartRoutingConfig: vi.fn(),
    getAutopilotTraceStats: vi.fn(),
    getAutopilotTraces: vi.fn(),
    updateSmartRoutingConfig: vi.fn()
  }
}))
const nullResponse = { traces: null, total: 0, hasMore: false } as unknown as AutopilotTraceListResponse
function render() {
  const names = [
    'VCard',
    'VCardTitle',
    'VCardText',
    'VIcon',
    'VSwitch',
    'VProgressCircular',
    'VAlert',
    'VChip',
    'VTable'
  ]
  const stubs = Object.fromEntries(names.map(name => [name, { template: '<div><slot /></div>' }]))
  return mount(AutopilotView, {
    global: {
      stubs: {
        ...stubs,
        VBtn: { template: '<button><slot /></button>' },
        AutopilotModePanel: true,
        AutopilotDiagnosePanel: true,
        AutopilotTraceStats: true,
        AutopilotTraceDetailDialog: true,
        EmptyState: true
      }
    }
  })
}
beforeEach(() => {
  vi.mocked(api.getSmartRoutingConfig)
    .mockReset()
    .mockResolvedValue({} as SmartRoutingConfig)
  vi.mocked(api.getAutopilotTraceStats)
    .mockReset()
    .mockResolvedValue({ totalCount: 0, comparedCount: 0, mismatchCount: 0 } as AutopilotTraceStats)
  vi.mocked(api.getAutopilotTraces).mockReset()
})
describe('Autopilot empty trace responses', () => {
  it('renders the actual table empty state when the server initially returns null', async () => {
    vi.mocked(api.getAutopilotTraces).mockResolvedValue(nullResponse)
    const view = render()
    await flushPromises()
    expect(view.findComponent(AutopilotTraceTable).props('traces')).toEqual([])
    expect(view.text()).toContain('autopilot.traceTable.empty')
    view.unmount()
  })
  it('keeps the table renderable when refreshing returns null', async () => {
    vi.mocked(api.getAutopilotTraces)
      .mockResolvedValueOnce({ traces: [], total: 0, hasMore: false })
      .mockResolvedValueOnce(nullResponse)
    const view = render()
    await flushPromises()
    await view.findComponent(AutopilotTraceTable).find('button').trigger('click')
    await flushPromises()
    expect(api.getAutopilotTraces).toHaveBeenCalledTimes(2)
    expect(view.findComponent(AutopilotTraceTable).props('traces')).toEqual([])
    expect(view.text()).toContain('autopilot.traceTable.empty')
    view.unmount()
  })
})
