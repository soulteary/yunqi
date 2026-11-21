import { useI18n } from '../../../i18n'
import type { CompsharePlanSnapshot, CompsharePlanUsageWindow, EndpointDetailItem, KimiCodeMoney, ManagedAccountCredential, MiMoTokenPlanQuota, MiMoTokenPlanSnapshot, VolcenginePlanBucket, VolcenginePlanUsage, VolcenginePlanUsageWindow } from '../../../services/api-types'
import { quotaRemainingColorClass } from '../../../utils/quotaColor'
import { buildKimiUsageSections } from '../../../utils/kimiPlanUsage'
import { buildQuotaTruth, unavailableQuotaTruth } from '../../../utils/quotaTruth'
import { buildMinimaxQuotaItems } from '../../../utils/minimaxEndpointUsage'
import type { UsageQuotaItem } from '../../../utils/usageQuotaItem'

// 渠道 Key 的摘要和详情共用同一份配额映射，不持有凭证或发起请求。
export function useKeyPlanUsage() {
  const { t } = useI18n()

  const compshareUsageItems = (plan: CompsharePlanSnapshot) => [
    { label: 'compshareConsoleCookie.fiveHourRemaining', window: plan.fiveHourUsage },
    { label: 'compshareConsoleCookie.weeklyRemaining', window: plan.weeklyUsage },
    { label: 'compshareConsoleCookie.monthlyRemaining', window: plan.monthlyUsage },
  ]

  const compshareNumberFormat = new Intl.NumberFormat()
  const compshareDateTimeFormat = new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })

  const compshareFormatRemaining = (window: CompsharePlanUsageWindow) => {
    const remaining = Math.max(0, window.limit - window.used)
    const remainingPercent = window.limit > 0 ? ((remaining / window.limit) * 100).toFixed(1) : '0.0'
    return `${t('volcengineAccessKey.remaining')} ${remainingPercent}% · ${compshareNumberFormat.format(remaining)}/${compshareNumberFormat.format(window.limit)}`
  }

  const compshareUsagePercent = (window: CompsharePlanUsageWindow) => {
    if (window.limit <= 0) return 0
    return Math.max(0, Math.min(100, (window.used / window.limit) * 100))
  }

  // 明细行：三个时间窗映射为统一余量行，进度条百分比与着色由 UsageQuotaRows 统一处理。
  const compshareUsageQuotaItems = (plan: CompsharePlanSnapshot): UsageQuotaItem[] =>
    compshareUsageItems(plan).map(item => ({
      key: item.label,
      label: t(item.label),
      usedPercent: compshareUsagePercent(item.window),
      value: compshareFormatRemaining(item.window),
      caption: `${t('compshareConsoleCookie.nextReset')} ${compshareFormatEpoch(item.window.nextResetAt)}`,
      ...buildQuotaTruth(compshareUsagePercent(item.window)),
    }))

  const compshareFormatEpoch = (value?: number) =>
    value && value > 0 ? compshareDateTimeFormat.format(new Date(value * 1000)) : '-'

  const compshareFormatDateTime = (value?: string) => {
    if (!value) return '-'
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? '-' : compshareDateTimeFormat.format(date)
  }

  const compsharePlanDisplayName = (plan: CompsharePlanSnapshot) => plan.displayName || plan.planName || plan.planCode

  // Key 行摘要：未绑定 Cookie 时提示绑定，否则展示套餐名与 5 小时/本周/月度余量。
  const compshareUsageSummary = (credential: ManagedAccountCredential): string => {
    if (!credential.hasCompshareConsoleCookie) return t('compshareConsoleCookie.notConfigured')
    const plan = credential.compsharePlan
    if (!plan) return t('compshareConsoleCookie.noUsageData')
    const parts = [compsharePlanDisplayName(plan)]
    if (plan.fiveHourUsage.limit > 0) {
      parts.push(`${t('compshareConsoleCookie.fiveHourRemaining')} ${compshareFormatRemaining(plan.fiveHourUsage)}`)
    }
    if (plan.weeklyUsage.limit > 0) {
      parts.push(`${t('compshareConsoleCookie.weeklyRemaining')} ${compshareFormatRemaining(plan.weeklyUsage)}`)
    }
    if (plan.monthlyUsage.limit > 0) {
      parts.push(`${t('compshareConsoleCookie.monthlyRemaining')} ${compshareFormatRemaining(plan.monthlyUsage)}`)
    }
    return parts.join(' · ')
  }

  // Key 行摘要：未绑定 Cookie 时提示绑定，否则展示套餐名与当前/月度余量。
  const mimoUsageSummary = (credential: ManagedAccountCredential): string => {
    if (!credential.hasMiMoConsoleCookie) return t('mimoConsoleCookie.notConfigured')
    const plan = credential.mimoTokenPlan
    if (!plan) return t('mimoConsoleCookie.noUsageData')
    const parts = [
      plan.planName,
      `${t('mimoConsoleCookie.currentRemaining')} ${formatMiMoQuota(plan.currentUsage)}`,
    ]
    if (plan.monthUsage.limit > 0) {
      parts.push(`${t('mimoConsoleCookie.monthRemaining')} ${formatMiMoQuota(plan.monthUsage)}`)
    }
    return parts.join(' · ')
  }

  // 明细行：按模型拆当前/周窗口两行，与健康中心共用 buildMinimaxQuotaItems 映射。
  const minimaxQuotaItems = (endpoint: EndpointDetailItem): UsageQuotaItem[] =>
    endpoint.miniMaxTokenPlanUsage ? buildMinimaxQuotaItems(endpoint.miniMaxTokenPlanUsage, t) : []

  const minimaxFormatDateTime = (value?: string) => {
    if (!value) return '-'
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString()
  }

  // Key 行摘要：展示各模型当前窗口剩余百分比。
  const minimaxUsageSummary = (endpoint: EndpointDetailItem): string => {
    if (endpoint.miniMaxTokenPlanUsageError) return endpoint.miniMaxTokenPlanUsageError
    const usage = endpoint.miniMaxTokenPlanUsage
    if (!usage?.models.length) return t('healthCenter.detail.noUsageData')
    return usage.models
      .map(model => `${model.modelName} 剩余 ${Math.max(0, Math.min(100, model.currentIntervalRemainingPercent)).toFixed(0)}%`)
      .join(' · ')
  }

  const formatMiMoQuota = (quota: MiMoTokenPlanQuota) => {
    const remainingPercent = Math.max(0, Math.min(100, (1 - quota.usedPercent) * 100)).toFixed(1)
    const remaining = Math.max(0, quota.limit - quota.used)
    return `剩余 ${remainingPercent}% · ${Intl.NumberFormat().format(remaining)} tokens`
  }

  // 明细行：当前/月度配额映射为统一余量行；当前周期行附带到期时间说明，月度额度为 0 时与摘要一致不展示。
  const mimoUsageItems = (plan: MiMoTokenPlanSnapshot): UsageQuotaItem[] => {
    const currentUsedPercent = Math.max(0, Math.min(100, plan.currentUsage.usedPercent * 100))
    const items: UsageQuotaItem[] = [
      {
        key: 'current',
        label: t('mimoConsoleCookie.currentRemaining'),
        usedPercent: currentUsedPercent,
        value: formatMiMoQuota(plan.currentUsage),
        caption: plan.currentPeriodEnd ? `${t('mimoConsoleCookie.expiresAt')} ${plan.currentPeriodEnd}` : '',
        ...buildQuotaTruth(currentUsedPercent),
      },
    ]
    if (plan.monthUsage.limit > 0) {
      const monthUsedPercent = Math.max(0, Math.min(100, plan.monthUsage.usedPercent * 100))
      items.push({
        key: 'month',
        label: t('mimoConsoleCookie.monthRemaining'),
        usedPercent: monthUsedPercent,
        value: formatMiMoQuota(plan.monthUsage),
        ...buildQuotaTruth(monthUsedPercent),
      })
    }
    return items
  }

  const planDisplayName = (plan?: string) => plan === 'agent_plan' ? 'Agent Plan' : 'Coding Plan'

  interface VolcengineUsageCell {
    labelKey: string
    text: string
    colorClass: string
    /** 0-100 已用百分比，用于明细行进度条；无法换算时缺省（该行不显示进度条）。 */
    usedPercent?: number
  }

  const numberFmt = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 })

  const hasVolcengineUsageData = (usage?: VolcenginePlanUsage) => {
    if (!usage || usage.error) return false
    return !!(usage.fiveHour || usage.daily || usage.weekly || usage.monthly)
  }

  const volcengineUsageColor = quotaRemainingColorClass

  // 单窗口展示：Agent Plan 显示剩余%与已用/额度，Coding Plan 显示剩余%与已用%。
  // usedPercent 供明细行渲染进度条；仅有用量计数、无法换算百分比时不返回。
  const volcengineWindowCell = (labelKey: string, win?: VolcenginePlanUsageWindow): VolcengineUsageCell | null => {
    if (!win) return null
    if (typeof win.usedPercent === 'number' && Number.isFinite(win.usedPercent)) {
      const usedPercent = Math.max(0, Math.min(100, win.usedPercent))
      const remainingPercent = 100 - usedPercent
      return {
        labelKey,
        text: `${t('volcengineAccessKey.remaining')} ${remainingPercent.toFixed(1)}% · ${t('volcengineAccessKey.used')} ${usedPercent.toFixed(1)}%`,
        colorClass: volcengineUsageColor(remainingPercent),
        usedPercent,
      }
    }
    if (win.quota && win.quota > 0) {
      const remaining = Math.max(0, win.quota - win.used)
      const remainingPercent = Math.max(0, Math.min(100, (remaining / win.quota) * 100))
      return {
        labelKey,
        text: `${t('volcengineAccessKey.remaining')} ${remainingPercent.toFixed(1)}% · ${numberFmt.format(win.used)}/${numberFmt.format(win.quota)}`,
        colorClass: volcengineUsageColor(remainingPercent),
        usedPercent: 100 - remainingPercent,
      }
    }
    return {
      labelKey,
      text: `${t('volcengineAccessKey.used')} ${numberFmt.format(win.used)}`,
      colorClass: '',
    }
  }

  const volcengineUsageWindows = (usage?: VolcenginePlanUsage): VolcengineUsageCell[] => {
    if (!usage) return []
    return [
      volcengineWindowCell('volcengineAccessKey.fiveHourWindow', usage.fiveHour),
      volcengineWindowCell('volcengineAccessKey.dailyWindow', usage.daily),
      volcengineWindowCell('volcengineAccessKey.weeklyWindow', usage.weekly),
      volcengineWindowCell('volcengineAccessKey.monthlyWindow', usage.monthly),
    ].filter((cell): cell is VolcengineUsageCell => cell !== null)
  }

  // 明细行：复用摘要单元格的文本与百分比，交给统一的 UsageQuotaRows 渲染。
  // 查询失败（凭证已配、provider 已知支持）时渲染 unavailable 占位行，
  // 真相徽章明示"获取失败"而非把错误静默折叠成纯文本。
  const volcengineUsageItems = (usage?: VolcenginePlanUsage): UsageQuotaItem[] => {
    if (usage?.error) {
      return [{
        key: 'volcengine-error',
        label: t('volcengineAccessKey.noUsageData'),
        value: usage.error,
        ...unavailableQuotaTruth(),
      }]
    }
    return volcengineUsageWindows(usage).map(cell => ({
      key: cell.labelKey,
      label: t(cell.labelKey),
      usedPercent: cell.usedPercent,
      value: cell.text,
      ...buildQuotaTruth(cell.usedPercent),
    }))
  }

  // 折叠摘要按窗口拆分为独立片段，复用 volcengineUsageColor 的阈值高亮低余量窗口。
  const volcengineUsageSummaryParts = (credential: ManagedAccountCredential): VolcengineUsageCell[] => {
    if (!credential.hasVolcengineAccessKey) {
      return [{ labelKey: '', text: t('volcengineAccessKey.notConfigured'), colorClass: '' }]
    }
    if (credential.volcenginePlanUsage?.error) {
      return [{ labelKey: '', text: credential.volcenginePlanUsage.error, colorClass: '' }]
    }
    const windows = volcengineUsageWindows(credential.volcenginePlanUsage)
    if (!windows.length) {
      return [{ labelKey: '', text: t('volcengineAccessKey.noUsageData'), colorClass: '' }]
    }
    return windows.map(window => ({
      labelKey: window.labelKey,
      text: `${t(window.labelKey)} ${window.text.split(' · ')[0]}`,
      colorClass: window.colorClass,
    }))
  }

  const formatVolcengineTime = (iso: string): string => {
    const date = new Date(iso)
    if (Number.isNaN(date.getTime())) return iso
    return date.toLocaleString()
  }

  // 其他套餐桶（主桶 personal 之外）：展示团队版席位与另一产品线的独立快照。
  const otherVolcengineBuckets = (credential: ManagedAccountCredential): VolcenginePlanBucket[] =>
    (credential.volcenginePlanBuckets ?? []).filter(
      bucket => !(bucket.product === credential.volcenginePlan && bucket.edition === 'personal'),
    )

  // 桶级用量紧凑文本：与折叠摘要同构（窗口标签 + 首段值），不占独立余量行。
  const volcengineBucketUsageText = (bucket: VolcenginePlanBucket): string =>
    volcengineUsageWindows(bucket.usage)
      .map(cell => `${t(cell.labelKey)} ${cell.text.split(' · ')[0]}`)
      .join(' · ')

  const kimiDateTimeFormat = new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })

  // 与 Kimi Code 官方 CLI 的 Plan usage 保持一致：5h limit（5 小时频限）+ Weekly limit（7 天频限），短窗口优先。
  // 明细行：把各用量区块（时间窗/订阅/赠送/加油包）映射为统一余量行，交给 UsageQuotaRows 渲染。
  const kimiUsageItems = (usage: Parameters<typeof buildKimiUsageSections>[0]): UsageQuotaItem[] =>
    buildKimiUsageSections(usage).map(section => {
      let value: string
      if (section.kind === 'booster') {
        value = section.wallet.allowTopup
          ? `${kimiFormatMoney(section.wallet.moneyLeft)} / ${kimiFormatMoney(section.wallet.moneyTotal)}`
          : t('kimiConsoleToken.boosterWalletDisabled')
      } else if ((section.kind === 'subscription' || section.kind === 'gift') && section.kimiUsedPercent >= 1) {
        // Kimi 通用额度未消耗时省略 Kimi/Code 拆分，避免与“已用”重复。
        value = `Kimi ${Math.round(section.kimiUsedPercent)}% · Code ${Math.round(section.codeUsedPercent)}% · ${t('kimiConsoleToken.percentUsed', { percent: Math.round(section.usedPercent) })}`
      } else {
        value = t('kimiConsoleToken.percentUsed', { percent: Math.round(section.usedPercent ?? 0) })
      }

      let caption = ''
      let captionTitle: string | undefined
      if (section.kind === 'window') {
        caption = t('kimiConsoleToken.resetsIn', { duration: kimiFormatCountdown(section.resetTime) })
        captionTitle = kimiFormatDateTime(section.resetTime)
      } else if (section.kind === 'subscription' || section.kind === 'gift') {
        caption = `${t('kimiConsoleToken.expiresAt')} ${kimiFormatExpireTime(section.balance.expireTime)}`
      } else if (section.kind === 'booster' && section.wallet.allowTopup) {
        caption = `${t('kimiConsoleToken.boosterWalletMonthlyUsed')} ${kimiFormatMoney(section.wallet.monthlyUsed)}`
      }

      return { key: section.key, label: t(section.labelKey), usedPercent: section.usedPercent, value, caption, captionTitle, ...buildQuotaTruth(section.usedPercent) }
    })

  const kimiFormatExpireTime = (value?: string) => {
    if (!value) return ''
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? '' : kimiDateTimeFormat.format(date)
  }

  // priceInCents 是分，按币种格式化为带符号金额（如 CNY -> ¥）。
  const kimiFormatMoney = (money: KimiCodeMoney) => {
    const amount = money.priceInCents / 100
    try {
      return new Intl.NumberFormat(undefined, {
        style: 'currency',
        currency: money.currency || 'CNY',
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
      }).format(amount)
    } catch {
      return `${amount.toFixed(2)} ${money.currency || ''}`.trim()
    }
  }

  const kimiFormatDateTime = (value?: string) => {
    if (!value) return '-'
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? '-' : kimiDateTimeFormat.format(date)
  }

  // 官方风格的相对重置倒计时，如 "5d 16h 29m"。
  const kimiFormatCountdown = (resetTime?: string) => {
    if (!resetTime) return '-'
    const resetAt = new Date(resetTime).getTime()
    if (Number.isNaN(resetAt)) return '-'
    const totalMinutes = Math.max(0, Math.floor((resetAt - Date.now()) / 60000))
    const days = Math.floor(totalMinutes / 1440)
    const hours = Math.floor((totalMinutes % 1440) / 60)
    const minutes = totalMinutes % 60
    const parts: string[] = []
    if (days > 0) parts.push(t('kimiConsoleToken.durationDay', { value: days }))
    if (days > 0 || hours > 0) parts.push(t('kimiConsoleToken.durationHour', { value: hours }))
    parts.push(t('kimiConsoleToken.durationMinute', { value: minutes }))
    return parts.join(' ')
  }

  // Key 行摘要与展开明细共享固定分项顺序，并按各分项剩余量独立着色。
  const kimiUsageSummaryParts = (credential: ManagedAccountCredential): VolcengineUsageCell[] => {
    if (!credential.hasKimiConsoleToken) {
      return [{ labelKey: '', text: t('kimiConsoleToken.notConfigured'), colorClass: '' }]
    }
    const usage = credential.kimiCodeUsage
    if (!usage) return [{ labelKey: '', text: t('kimiConsoleToken.noUsageData'), colorClass: '' }]

    const sections = buildKimiUsageSections(usage)
    if (!sections.length) {
      return [{ labelKey: '', text: t('kimiConsoleToken.noUsageData'), colorClass: '' }]
    }

    return sections.map(section => {
      const label = t(section.labelKey)
      if (section.kind === 'booster') {
        if (!section.wallet.allowTopup) {
          return {
            labelKey: section.labelKey,
            text: `${label} ${t('kimiConsoleToken.boosterWalletDisabled')}`,
            colorClass: '',
          }
        }
        if (section.usedPercent === undefined) {
          return {
            labelKey: section.labelKey,
            text: `${label} ${kimiFormatMoney(section.wallet.moneyLeft)} / ${kimiFormatMoney(section.wallet.moneyTotal)}`,
            colorClass: '',
          }
        }
      }
      const usedPercent = section.usedPercent
      if (usedPercent === undefined) {
        return { labelKey: '', text: t('kimiConsoleToken.noUsageData'), colorClass: '' }
      }
      const remainingPercent = Math.max(0, Math.min(100, 100 - usedPercent))
      // 摘要保持「标签 + 剩余%」的短格式，避免 Kimi/Code 拆分把行撑爆截断。
      return {
        labelKey: section.labelKey,
        text: `${label} ${t('kimiConsoleToken.percentRemaining', { percent: Math.round(remainingPercent) })}`,
        colorClass: quotaRemainingColorClass(remainingPercent),
      }
    })
  }

  return {
    compshareUsageQuotaItems,
    compshareFormatEpoch,
    compshareFormatDateTime,
    compsharePlanDisplayName,
    compshareUsageSummary,
    mimoUsageSummary,
    minimaxQuotaItems,
    minimaxFormatDateTime,
    minimaxUsageSummary,
    mimoUsageItems,
    planDisplayName,
    hasVolcengineUsageData,
    volcengineUsageItems,
    volcengineUsageSummaryParts,
    formatVolcengineTime,
    otherVolcengineBuckets,
    volcengineBucketUsageText,
    kimiUsageItems,
    kimiFormatDateTime,
    kimiUsageSummaryParts,
  }
}
