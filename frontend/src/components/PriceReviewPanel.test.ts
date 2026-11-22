// @vitest-environment jsdom
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PriceReviewPanel from './PriceReviewPanel.vue'
import { ApiError } from '../services/api-helpers'
const mocks = vi.hoisted(() => ({ getPriceReview: vi.fn(), checkPresetPrices: vi.fn(), approvePresetPrices: vi.fn(), rollbackPresetPrices: vi.fn() }))
vi.mock('../services/api', async () => ({ api: mocks, ApiError: (await import('../services/api-helpers')).ApiError }))
vi.mock('../i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const pass = defineComponent({ template: '<div><slot /></div>' })
const checkbox = defineComponent({ props: ['modelValue', 'label'], emits: ['update:modelValue'], template: '<input type="checkbox" :checked="modelValue" @change="$emit(\'update:modelValue\', !modelValue)" />' })
function render() { return mount(PriceReviewPanel, { global: { stubs: { VCard: pass, VCardTitle: pass, VCardText: pass, VTable: pass, VAlert: pass, VSelect: pass, VPagination: pass, VCheckbox: checkbox, VBtn: { props: ['disabled'], template: '<button :disabled="disabled"><slot /></button>' } } } }) }
const pending = { reviewId: 'review-original', beforeBundleHash: 'before', beforeVersion: 'old', afterVersion: 'new', dataVersion: 'source-v2', sourceUrl: 'https://example.test/index', sourceDigest: 'a'.repeat(64), signatureVerified: false, thresholdPercent: 50, changeCount: 1, previewTruncated: false, requiresAnomalyAcknowledgement: true, changes: [{ model: 'sample-model', before: { outputPrice: 1 }, after: { outputPrice: 3 }, anomaly: true, reasons: ['price_jump_or_dimension_change'] }] }
function state() { return { activeVersion: 'old', activeDataVersion: 'source-v1', activeBundleHash: 'before', rollbackScope: 'entire bundle', pending: { ...pending }, history: [], audit: [] } }
describe('public price approval', () => {
 beforeEach(() => { vi.clearAllMocks() })
 it('does no background fetch and requires the anomalous proposal acknowledgement', async () => {
  const value = state(); mocks.getPriceReview.mockResolvedValue({ persisted: false, state: value }); mocks.approvePresetPrices.mockResolvedValue({ persisted: true, state: { ...value, activeVersion: 'new', pending: undefined } })
  const wrapper = render(); expect(mocks.getPriceReview).not.toHaveBeenCalled()
  await wrapper.get('button').trigger('click'); await flushPromises()
  const approve = wrapper.findAll('button').find(button => button.text() === 'priceReview.approve')!
  expect(approve.attributes('disabled')).toBeDefined()
  await wrapper.get('input').setValue(true)
  await approve.trigger('click'); await flushPromises()
  expect(mocks.approvePresetPrices).toHaveBeenCalledWith('review-original', true)
  expect(wrapper.text()).toContain('priceReview.noPending')
 })
 it('replaces stale data with the actual server state and clears old acknowledgement', async () => {
  const value = state(); mocks.getPriceReview.mockResolvedValue({ persisted: false, state: value })
  mocks.approvePresetPrices.mockRejectedValue(new ApiError('stale', 409, { state: { ...value, pending: { ...pending, reviewId: 'review-new' } } }))
  const wrapper = render(); await wrapper.get('button').trigger('click'); await flushPromises()
  await wrapper.get('input').setValue(true)
  await wrapper.findAll('button').find(button => button.text() === 'priceReview.approve')!.trigger('click'); await flushPromises()
  expect(wrapper.text()).toContain('stale')
  expect((wrapper.get('input').element as HTMLInputElement).checked).toBe(false)
  expect(wrapper.findAll('button').find(button => button.text() === 'priceReview.approve')!.attributes('disabled')).toBeDefined()
 })
})
