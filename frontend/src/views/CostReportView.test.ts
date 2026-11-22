// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import { api } from '@/services/api'
import CostReportView from './CostReportView.vue'

vi.mock('@/i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/services/api', () => ({ api: { getCostReport: vi.fn(), getCostRequestHistory: vi.fn() } }))

afterEach(() => {
  vi.unstubAllGlobals()
})

it('downloads a CSV with escaped model names and safe spreadsheet cells from the real export button', async () => {
  const payloads: string[] = []
  vi.stubGlobal(
    'Blob',
    class {
      constructor(parts: string[]) {
        payloads.push(parts.join(''))
      }
    }
  )
  const createObjectURL = vi.fn(() => 'blob:cost-report')
  const revokeObjectURL = vi.fn()
  vi.stubGlobal('URL', { createObjectURL, revokeObjectURL })
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  vi.mocked(api.getCostReport).mockResolvedValue({
    groupBy: 'user',
    duration: '7d',
    apiType: 'messages',
    rows: [
      {
        groupKey: 'company,model\n"v2"',
        totalRequests: 1,
        successCount: 1,
        inputTokens: 12,
        outputTokens: 3,
        cacheCreationTokens: 0,
        cacheReadTokens: 0,
        listCostUSD: 0.01,
        effectiveCostUSD: 0.01,
        unpricedModels: ['=1+1']
      }
    ]
  })
  const names = ['VCard', 'VCardText', 'VRow', 'VCol', 'VTable', 'VTooltip', 'VIcon']
  const view = mount(CostReportView, {
    global: {
      stubs: {
        ...Object.fromEntries(names.map(name => [name, { template: '<div><slot /></div>' }])),
        VBtn: { props: ['disabled'], template: '<button :disabled="disabled"><slot /></button>' },
        VSelect: true,
        VChip: { template: '<span><slot /></span>' }
      }
    }
  })
  try {
    await flushPromises()
    await view
      .findAll('button')
      .find(button => button.text() === 'costReport.exportCsv')!
      .trigger('click')
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

it('keeps principal scope aligned across model drilldown, request history and CSV', async () => {
  const principal = 'p_' + 'a'.repeat(64)
  const label = 'principal:aaaaaaaa...aaaa'
  const row = {
    groupKey: principal,
    groupLabel: label,
    totalRequests: 2,
    successCount: 2,
    inputTokens: 2,
    outputTokens: 1,
    cacheCreationTokens: 0,
    cacheReadTokens: 0,
    listCostUSD: 0.5,
    effectiveCostUSD: 0.5
  }
  vi.mocked(api.getCostReport).mockImplementation(async groupBy => ({
    groupBy: groupBy as 'principal' | 'model',
    duration: '7d',
    apiType: 'messages',
    since: 10,
    until: 20,
    rows: groupBy === 'model' ? [{ ...row, groupKey: 'model-a', groupLabel: undefined }] : [row]
  }))
  vi.mocked(api.getCostRequestHistory).mockResolvedValue({
    retentionDays: 7,
    records: [
      {
        id: 1,
        timestamp: 15,
        type: 'messages',
        principalId: principal,
        principalLabel: label,
        model: 'model-a',
        channelUid: 'channel',
        correlationId: 'request-opaque',
        success: true,
        inputTokens: 2,
        outputTokens: 1
      }
    ]
  })
  const payloads: string[] = []
  vi.stubGlobal(
    'Blob',
    class {
      constructor(parts: string[]) {
        payloads.push(parts.join(''))
      }
    }
  )
  vi.stubGlobal('URL', { createObjectURL: () => 'blob:cost', revokeObjectURL: vi.fn() })
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  const view = mount(CostReportView, {
    global: {
      stubs: {
        PriceReviewPanel: true,
        VSelect: true,
        ...Object.fromEntries(
          ['VCard', 'VCardText', 'VRow', 'VCol', 'VTable', 'VTooltip', 'VIcon', 'VChip'].map(name => [
            name,
            { template: '<div><slot /></div>' }
          ])
        ),
        VBtn: { props: ['disabled'], template: '<button :disabled="disabled"><slot /></button>' }
      }
    }
  })
  const button = (text: string) => view.findAll('button').find(item => item.text() === text)!
  try {
    await flushPromises()
    await button('costReport.modelsForPrincipal').trigger('click')
    await flushPromises()
    expect(api.getCostReport).toHaveBeenLastCalledWith('model', '7d', 'messages', principal)
    await button('costReport.viewRequests').trigger('click')
    await flushPromises()
    expect(api.getCostRequestHistory).toHaveBeenLastCalledWith(principal, 'messages', 10, 20, '')
    expect(view.text()).toContain('request-opaque')
    await button('costReport.exportCsv').trigger('click')
    expect(payloads[0]).toContain(principal)
    expect(payloads[0]).toContain(label)
    expect(payloads[0]).toContain('model-a')
    await button('costReport.clearPrincipal').trigger('click')
    await flushPromises()
    expect(api.getCostReport).toHaveBeenLastCalledWith('principal', '7d', 'messages', '')
    expect(view.text()).not.toContain('request-opaque')
  } finally {
    view.unmount()
    click.mockRestore()
  }
})

it('does not relabel or export a stale principal response after clearing the scope', async () => {
  const principal = 'p_' + 'b'.repeat(64)
  const row = {
    groupKey: principal,
    groupLabel: 'principal:bbbbbbbb...bbbb',
    totalRequests: 1,
    successCount: 1,
    inputTokens: 1,
    outputTokens: 1,
    cacheCreationTokens: 0,
    cacheReadTokens: 0,
    listCostUSD: 0.1,
    effectiveCostUSD: 0.1
  }
  const response = { groupBy: 'principal' as const, duration: '7d', apiType: 'messages', rows: [row] }
  let late!: (value: typeof response) => void
  vi.mocked(api.getCostReport)
    .mockResolvedValueOnce(response)
    .mockImplementationOnce(
      () =>
        new Promise(resolve => {
          late = resolve
        })
    )
    .mockResolvedValueOnce(response)
  const view = mount(CostReportView, {
    global: {
      stubs: {
        PriceReviewPanel: true,
        VSelect: true,
        ...Object.fromEntries(
          ['VCard', 'VCardText', 'VRow', 'VCol', 'VTable', 'VTooltip', 'VIcon', 'VChip'].map(name => [
            name,
            { template: '<div><slot /></div>' }
          ])
        ),
        VBtn: { props: ['disabled'], template: '<button :disabled="disabled"><slot /></button>' }
      }
    }
  })
  try {
    await flushPromises()
    await view
      .findAll('button')
      .find(item => item.text() === 'costReport.modelsForPrincipal')!
      .trigger('click')
    expect(
      view
        .findAll('button')
        .find(item => item.text() === 'costReport.exportCsv')!
        .attributes('disabled')
    ).toBeDefined()
    await view
      .findAll('button')
      .find(item => item.text() === 'costReport.clearPrincipal')!
      .trigger('click')
    await flushPromises()
    late({ ...response, rows: [{ ...row, groupKey: 'stale-model-from-old-scope' }] })
    await flushPromises()
    expect(view.text()).not.toContain('stale-model-from-old-scope')
    expect(view.text()).toContain('principal:bbbbbbbb...bbbb')
  } finally {
    view.unmount()
  }
})

it('exports unknown missing-model pricing as partial rather than a complete free bill', async () => {
  const row = {
    groupKey: 'unknown',
    groupLabel: 'unknown',
    totalRequests: 1,
    successCount: 1,
    inputTokens: 8,
    outputTokens: 0,
    cacheCreationTokens: 0,
    cacheReadTokens: 0,
    listCostUSD: 0,
    effectiveCostUSD: 0,
    pricingComplete: false
  }
  vi.mocked(api.getCostReport).mockImplementation(async groupBy => ({
    groupBy: groupBy as 'principal' | 'model',
    duration: '7d',
    apiType: 'messages',
    since: 10,
    until: 20,
    rows: groupBy === 'model' ? [{ ...row, groupKey: '', groupLabel: undefined }] : [row]
  }))
  const payloads: string[] = []
  vi.stubGlobal(
    'Blob',
    class {
      constructor(parts: string[]) {
        payloads.push(parts.join(''))
      }
    }
  )
  vi.stubGlobal('URL', { createObjectURL: () => 'blob:cost', revokeObjectURL: vi.fn() })
  const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  const view = mount(CostReportView, {
    global: {
      stubs: {
        PriceReviewPanel: true,
        VSelect: true,
        ...Object.fromEntries(
          ['VCard', 'VCardText', 'VRow', 'VCol', 'VTable', 'VTooltip', 'VIcon', 'VChip'].map(name => [
            name,
            { template: '<div><slot /></div>' }
          ])
        ),
        VBtn: { props: ['disabled'], template: '<button :disabled="disabled"><slot /></button>' }
      }
    }
  })
  try {
    await flushPromises()
    expect(view.text()).toContain('$0.000000+')
    await view
      .findAll('button')
      .find(item => item.text() === 'costReport.modelsForPrincipal')!
      .trigger('click')
    await flushPromises()
    expect(api.getCostReport).toHaveBeenLastCalledWith('model', '7d', 'messages', 'unknown')
    expect(view.text()).toContain('$0.000000+')
    await view
      .findAll('button')
      .find(item => item.text() === 'costReport.exportCsv')!
      .trigger('click')
    expect(payloads[0]).toContain('0.000000,costReport.pricingStatus.partial')
    expect(payloads[0]).toContain('unknown,costReport.unknownPrincipal')
  } finally {
    view.unmount()
    click.mockRestore()
  }
})
