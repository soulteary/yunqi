import type { ChannelProtocolRoute } from '../services/api'

export const DESKTOP_TIERS = ['sonnet', 'opus', 'haiku', 'fable', 'mythos'] as const

const ordered = (value: Record<string, string> = {}) =>
  Object.entries(value).sort(([a], [b]) => a.localeCompare(b))

export function buildDesktopTierUpdates(original: ChannelProtocolRoute[], edited: ChannelProtocolRoute[]) {
  return edited.flatMap(route => {
    if (route.kind !== 'messages' || !route.channelUid || route.claudeDesktopModelTiers === undefined) return []
    const before = original.find(item => item.channelUid === route.channelUid && item.kind === route.kind)
    if (!before || JSON.stringify(ordered(before.claudeDesktopModelTiers)) === JSON.stringify(ordered(route.claudeDesktopModelTiers))) return []
    return [{ channelUid: route.channelUid, kind: route.kind, tiers: { ...route.claudeDesktopModelTiers } }]
  })
}

/** Match the channel capability resolver's prefix/suffix/contains rules. */
export function selectedDesktopTier(route: ChannelProtocolRoute, model: string): string {
  if (Object.prototype.hasOwnProperty.call(route.claudeDesktopModelTiers ?? {}, model)) return route.claudeDesktopModelTiers![model] ?? ''
  if (Object.prototype.hasOwnProperty.call(route.claudeDesktopTierRules ?? {}, model)) return route.claudeDesktopTierRules![model] ?? ''
  const rules = Object.entries(route.claudeDesktopTierRules ?? {}).sort(([a], [b]) => b.length - a.length || (a < b ? -1 : a > b ? 1 : 0))
  for (const [pattern, tier] of rules) {
    const value = pattern.startsWith('!') ? pattern.slice(1).trim() : pattern
    const stem = value.replace(/^\*|\*$/g, '')
    const matches = value === '*' || (value.startsWith('*') && value.endsWith('*') ? model.includes(stem)
      : value.startsWith('*') ? model.endsWith(stem) : value.endsWith('*') ? model.startsWith(stem) : value === model)
    if (matches) return tier
  }
  return ''
}
