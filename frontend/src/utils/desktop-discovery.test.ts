import { describe, expect, it } from 'vitest'
import { createModelCapabilityRow, modelCapabilitiesToRows, modelCapabilityRowsToRecord } from './channelPayload'

describe('Desktop discovery tier configuration', () => {
  it('preserves explicit tiers and context through channel form serialization', () => {
    const capabilities = {
      'deepseek-v4-pro': { anthropicFamilyTier: 'sonnet', contextWindowTokens: 1048576, maxOutputTokens: 393216 },
      'deepseek-flash': { anthropicFamilyTier: 'haiku', contextWindowTokens: 1048576 },
    }
    let id = 0
    expect(modelCapabilityRowsToRecord(modelCapabilitiesToRows(capabilities, () => ++id))).toEqual(capabilities)
  })

  it('does not assign a tier from a provider name', () => {
    const row = createModelCapabilityRow(1, 'deepseek-flash')
    expect(modelCapabilityRowsToRecord([row])).toEqual({ 'deepseek-flash': {} })
  })

  it('normalizes an explicit tier and rejects invalid or unnamed mappings', () => {
    const row = createModelCapabilityRow(1, 'deepseek-v4-pro', { anthropicFamilyTier: ' Sonnet ' })
    expect(modelCapabilityRowsToRecord([row])).toEqual({ 'deepseek-v4-pro': { anthropicFamilyTier: 'sonnet' } })
    expect(modelCapabilityRowsToRecord([{ ...row, anthropicFamilyTier: 'deepseek' }])).toBeNull()
    expect(modelCapabilityRowsToRecord([{ ...row, model: '' }])).toBeNull()
  })
})
