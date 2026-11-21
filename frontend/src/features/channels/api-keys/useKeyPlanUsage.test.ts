import { describe, expect, it, vi } from 'vitest'
import type { ManagedAccountCredential, MiMoTokenPlanSnapshot, VolcenginePlanUsage } from '../../../services/api-types'
import { useKeyPlanUsage } from './useKeyPlanUsage'

vi.mock('../../../i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const usage = useKeyPlanUsage()

describe('channel Key plan presentation', () => {
  it.each([
    [80, 'approaching_limit', 80],
    [125, 'exhausted', 100],
    [-20, 'healthy', 0],
  ])('keeps summary, details and quota truth consistent for %s percent used', (usedPercent, truthLevel, clamped) => {
    const snapshot: VolcenginePlanUsage = { fetchedAt: '2026-11-21', fiveHour: { used: 1, usedPercent } }
    const credential: ManagedAccountCredential = { credentialUid: 'cred', keyMask: 'masked', hasVolcengineAccessKey: true, volcenginePlanUsage: snapshot }
    const [detail] = usage.volcengineUsageItems(snapshot)
    const [summary] = usage.volcengineUsageSummaryParts(credential)
    expect(detail).toMatchObject({ usedPercent: clamped, truthLevel, truthSource: 'provider_api' })
    expect(summary?.text).toContain(`${(100 - clamped).toFixed(1)}%`)
    expect(detail?.value).toContain(`${(100 - clamped).toFixed(1)}%`)
  })

  it('keeps failed queries unavailable and count-only windows unknown rather than empty or exhausted', () => {
    expect(usage.volcengineUsageItems({ fetchedAt: '', error: 'upstream unavailable' })).toEqual([
      expect.objectContaining({ value: 'upstream unavailable', truthLevel: 'unavailable', truthSource: 'provider_api' }),
    ])
    expect(usage.volcengineUsageItems({ fetchedAt: '', daily: { used: 42 } })).toEqual([
      expect.objectContaining({ usedPercent: undefined, truthLevel: 'unknown' }),
    ])
  })

  it('omits zero monthly allowances while preserving MiMo current quota and expiry', () => {
    const plan: MiMoTokenPlanSnapshot = {
      planCode: 'coding', planName: 'Coding', currentPeriodEnd: '2026-12-01', expired: false, validatedAt: '',
      currentUsage: { used: 12, limit: 10, usedPercent: 1.2 },
      monthUsage: { used: 0, limit: 0, usedPercent: 0 },
    }
    const items = usage.mimoUsageItems(plan)
    expect(items).toHaveLength(1)
    expect(items[0]).toMatchObject({ key: 'current', usedPercent: 100, truthLevel: 'exhausted' })
    expect(items[0]?.value).toContain('0.0%')
    expect(items[0]?.caption).toContain('2026-12-01')
    const credential: ManagedAccountCredential = { credentialUid: 'cred', keyMask: 'masked', hasMiMoConsoleCookie: true, mimoTokenPlan: plan }
    expect(usage.mimoUsageSummary(credential)).not.toContain('mimoConsoleCookie.monthRemaining')
  })

  it('keeps team and other-product quota buckets separate from the selected personal plan', () => {
    const credential: ManagedAccountCredential = {
      credentialUid: 'cred', keyMask: 'masked', volcenginePlan: 'coding_plan',
      volcenginePlanBuckets: [
        { product: 'coding_plan', edition: 'personal' },
        { product: 'coding_plan', edition: 'team' },
        { product: 'agent_plan', edition: 'personal' },
      ],
    }
    expect(usage.otherVolcengineBuckets(credential)).toEqual(credential.volcenginePlanBuckets?.slice(1))
  })
})
