import { describe, expect, it } from 'vitest'
import { buildDesktopTierUpdates } from './desktopModelTiers'
import type { ChannelProtocolRoute } from '../services/api'

const route: ChannelProtocolRoute = { kind: 'messages', index: 3, channelUid: 'stable', name: 'DeepSeek', serviceType: 'claude', claudeDesktopModelTiers: { a: 'sonnet', b: 'haiku' } }
describe('Desktop tier patches', () => {
  it('ignores key order and unchanged mappings', () => {
    expect(buildDesktopTierUpdates([route], [{ ...route, claudeDesktopModelTiers: { b: 'haiku', a: 'sonnet' } }])).toEqual([])
  })
  it('sends explicit clears by UID despite reordered indices', () => {
    expect(buildDesktopTierUpdates([route], [{ ...route, index: 8, claudeDesktopModelTiers: { a: '' } }])).toEqual([{ channelUid: 'stable', kind: 'messages', tiers: { a: '' } }])
    expect(buildDesktopTierUpdates([route], [{ ...route, claudeDesktopModelTiers: {} }])).toEqual([{ channelUid: 'stable', kind: 'messages', tiers: {} }])
  })
  it('omits missing values, non-Messages routes and unknown UIDs', () => {
    expect(buildDesktopTierUpdates([route], [{ ...route, claudeDesktopModelTiers: undefined }, { ...route, kind: 'chat' }, { ...route, channelUid: 'unknown' }, { ...route, channelUid: undefined }])).toEqual([])
  })
})
