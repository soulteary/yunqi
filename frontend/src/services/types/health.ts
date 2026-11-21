// 渠道级上游余额探测配置（对应后端 ChannelBalanceCheckConfig）
export interface ChannelBalanceCheckConfig {
  enabled?: boolean // 渠道级开关（nil=继承全局/托管默认：newapi 托管渠道默认开）
  provider?: string // 上游类型：auto | newapi_token | sub2api | newapi_billing（空=auto）
  intervalMinutes?: number // 探测间隔（分钟，0=继承全局默认 6h，下限 30）
  quotaPerUnit?: number // new-api quota 点→USD 换算比率（0=默认 500000）
}

// 余额探测端点类型选项（label 由 i18n 提供）
export const BALANCE_PROVIDER_VALUES = ['auto', 'newapi_token', 'sub2api', 'newapi_billing'] as const

// GET /api/{type}/channels/:id/balance 响应
export interface ChannelBalanceView {
  channelType: string
  channelId: string
  channelUid?: string
  policy: {
    enabled: boolean
    intervalMinutes: number
    provider: string
    timeoutMs: number
    quotaPerUnit: number
  }
  records: Array<{
    keyMask: string
    status: string
    lastCheckAtMs: number
    consecutiveFailures: number
    detail: {
      provider?: string
      scope?: string
      remaining?: number
      used?: number
      limit?: number
      originalUnit?: string
      usd?: number
      unlimited?: boolean
      error?: string
    }
    rateWindows?: Array<{ window: string; limit: number; used: number; remaining: number; resetAtMs?: number }>
  }>
  quotaValues: Array<{
    dimension: string
    limit?: number
    used?: number
    remaining?: number
    unit?: string
    source: string
    resetAtMs?: number
    observedAtMs?: number
  }>
}

// ============== 健康中心类型 ==============

export type HealthState = 'unknown' | 'healthy' | 'degraded' | 'limited' | 'misconfigured' | 'dead'

export interface HealthCenterOverview {
  totalChannels: number
  totalEndpoints: number
  stateCounts: Record<HealthState, number>
}

export interface ChannelHealthItem {
  channelUid: string
  channelId: number
  channelKind: string
  channelName?: string
  aggState: HealthState
  endpointCount: number
  healthyCount: number
  degradedCount: number
  limitedCount: number
  deadCount: number
  unknownCount: number
  avgSuccessRate?: number
  speedTier?: 'fast' | 'normal' | 'slow'
  connectSampleCount?: number
  p95ConnectLatencyMs?: number
  firstByteSampleCount?: number
  p95FirstByteLatencyMs?: number
  // Forward-compat: origin/pool tags for card badge system (§8.2).
  // These fields may not be present in all API versions; consumers must null-check.
  originTier?: 'first' | 'second' | 'third' | 'unknown'
  poolTag?: 'free' | 'temp' | ''
}

export interface HealthCenterChannelsResponse {
  channels: ChannelHealthItem[]
}

export interface EndpointDetailItem {
  endpointUid: string
  channelUid: string
  channelKind: string
  baseUrl: string
  keyHash: string
  keyMask?: string
  healthState: HealthState
  healthConfidence: number
  healthEvidence?: string
  suggestedAction?: string
  qualityTier?: string
  stabilityTier?: string
  speedTier?: string
  successRate15m?: number
  successRate1h?: number
  p95LatencyMs?: number
  connectSampleCount?: number
  p95ConnectLatencyMs?: number
  firstByteSampleCount?: number
  p95FirstByteLatencyMs?: number
  consecutiveFail: number
  lastSuccessAt?: string
  updatedAt?: string
  tokenPlanUsageSupported?: boolean
  miniMaxTokenPlanUsage?: MiniMaxTokenPlanUsage
  miniMaxTokenPlanUsageError?: string
}

export interface MiniMaxTokenPlanModelUsage {
  modelName: string
  currentIntervalUsageCount: number
  currentIntervalTotalCount: number
  currentIntervalRemainingPercent: number
  currentWeeklyUsageCount: number
  currentWeeklyTotalCount: number
  currentWeeklyRemainingPercent: number
  remainsTimeMs: number
  weeklyStartTime?: string
  weeklyEndTime?: string
}

export interface MiniMaxTokenPlanUsage {
  models: MiniMaxTokenPlanModelUsage[]
  fetchedAt: string
  sourceUrl: string
}

export interface TokenPlanUsageRefreshResponse {
  usage: MiniMaxTokenPlanUsage
  cached: boolean
}

export interface HealthCenterEndpointsResponse {
  channelUid: string
  endpoints: EndpointDetailItem[]
}
