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
  groupBy: 'user' | 'model' | 'key'
  apiType: string
  duration: string
  rows: CostReportRow[]
}
