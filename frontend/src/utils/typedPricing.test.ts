import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'
import { createModelCapabilityRow, modelCapabilityRowsToRecord, parseModelCapabilitiesText } from './channelPayload'
describe('typed pricing edit contracts', () => {
 it('round trips TTL, reasoning, per-image prices and tiers including explicit zero', () => {
  const pricing = { inputCacheMissPrice: 1, cacheCreation5mPrice: 0, cacheCreation1hPrice: 4, reasoningOutputPrice: 2, imagePrices: [{ quality: 'high', size: '1024x1024', price: 0 }], tiers: [{ inputTokensAbove: 100, cacheCreation5mPrice: 3, reasoningOutputPrice: 0 }] }
  const parsed = parseModelCapabilitiesText(JSON.stringify({ model: { pricing } }))!
  expect(parsed.model?.pricing).toEqual(pricing)
  const row = reactive(createModelCapabilityRow(1, 'model', parsed.model))
  row.contextWindowTokens = 1000
  const saved = modelCapabilityRowsToRecord([row])!
  expect(saved.model?.pricing).toMatchObject(pricing)
  expect(saved.model?.contextWindowTokens).toBe(1000)
  row.typedPricing!.imagePrices![0]!.price = 9
  expect(parsed.model?.pricing?.imagePrices?.[0]?.price).toBe(0)
 })
 it('rejects negative dimensions and unbounded image schedules', () => {
  expect(parseModelCapabilitiesText(JSON.stringify({ m: { pricing: { cacheCreation1hPrice: -1 } } }))).toBeNull()
  expect(parseModelCapabilitiesText(JSON.stringify({ m: { pricing: { imagePrices: Array.from({ length: 65 }, () => ({ price: 1 })) } } }))).toBeNull()
 })
})
