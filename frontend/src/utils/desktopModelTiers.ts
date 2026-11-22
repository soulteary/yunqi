import type { ChannelProtocolRoute } from '../services/api'

export const DESKTOP_TIERS = ['sonnet', 'opus', 'haiku', 'fable', 'mythos'] as const

const ordered = (value: Record<string, string> = {}) =>
  Object.entries(value).sort(([a], [b]) => a.localeCompare(b))

export function buildDesktopTierUpdates(original: ChannelProtocolRoute[], edited: ChannelProtocolRoute[]) {
  return edited.flatMap(route => {
    if (route.kind !== 'messages' || !route.channelUid || route.claudeDesktopModelTiers === undefined) return []
    const before = original.find(item => item.channelUid === route.channelUid && item.kind === route.kind)
    if (!before || JSON.stringify(ordered(before.claudeDesktopModelTiers)) === JSON.stringify(ordered(route.claudeDesktopModelTiers))) return []
    return [{ channelUid: route.channelUid, kind: route.upstreamKind ?? route.kind, tiers: { ...route.claudeDesktopModelTiers } }]
  })
}
