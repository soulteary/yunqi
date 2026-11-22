// ============== 汇率与 NewAPI Key 状态类型 ==============

export interface ExchangeRateQuote {
  sourceAmount: number
  sourceUnit: string
  targetAmount: number
  targetUnit: string
  updatedAt?: string
  note?: string
}

export interface ExchangeRateSnapshot {
  version: number
  usdUnitPrices: Record<string, number>
  builtAt: string
}

export interface ExchangeRatesResponse {
  quotes: ExchangeRateQuote[]
  snapshot?: ExchangeRateSnapshot
  source?: string
  version?: number
}

export interface ExchangeRatesReplaceRequest {
  quotes: ExchangeRateQuote[]
  expectedSnapshotVersion?: number
}

export interface KeyMultiplierPatch {
  groupMultiplier?: number | null
  consumptionPolicy?: 'normal' | 'opportunistic' | null
}

export interface KeyMultiplierResponse {
  keyUid: string
  group: string
  remoteMultiplier?: number
  groupMultiplier?: number
  maxMultiplier?: number
  consumptionPolicy?: 'normal' | 'opportunistic'
  effectiveCostClass?: 'zero' | 'discounted' | 'standard' | 'premium' | 'unknown'
  status: string
  reason: string
  eligible: boolean
  updatedAt?: string
  expiresAt?: string
}

// ============== 成本报表（Phase 4 Item 2） ==============

export interface CostReportRow {
  groupLabel?: string

  groupKey: string
  totalRequests: number
  successCount: number
  inputTokens: number
  outputTokens: number
  cacheCreationTokens: number
  cacheReadTokens: number
  listCostUSD: number
  effectiveCostUSD: number
  pricingComplete?: boolean
  unpricedModels?: string[]
  zeroCostCount?: number
  configuredMultiplierCount?: number
  subscriptionCostCount?: number
  unpricedCostCount?: number
  // 请求侧压缩统计（RTK 模式）
  compressedRequests?: number
  originalTokensSaved?: number
  compressedTokensAfter?: number
  compressionFallbackCount?: number
  compressionSavingsPct?: number
}

export interface CostReportResponse {
  groupBy: 'principal' | 'user' | 'model' | 'key'
  apiType: string
  duration: string
  principalId?: string
  principalLabel?: string
  since?: number
  until?: number
  rows: CostReportRow[]
}

export interface CostRequestHistoryEntry {
  id: number
  timestamp: number
  type: string
  principalId: string
  principalLabel: string
  model: string
  channelUid: string
  correlationId: string
  success: boolean
  inputTokens: number
  outputTokens: number
}
export interface CostRequestHistoryPage {
  records: CostRequestHistoryEntry[]
  nextCursor?: string
  retentionDays: number
}
