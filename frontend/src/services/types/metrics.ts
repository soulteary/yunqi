import type { ChannelKind, CircuitState } from './common'

// 渠道指标
// 分时段统计
export interface TimeWindowStats {
  requestCount: number
  // 真实用户请求数（COUNT DISTINCT correlation_id）；omitempty，缺省时等同 requestCount 口径
  userRequestCount?: number
  successCount: number
  failureCount: number
  successRate: number
  inputTokens?: number
  outputTokens?: number
  cacheCreationTokens?: number
  cacheReadTokens?: number
  cacheHitRate?: number
}

export interface ChannelMetrics {
  channelIndex: number
  routeKind?: ChannelKind
  requestCount: number
  successCount: number
  failureCount: number
  successRate: number // 0-100
  errorRate: number // 0-100
  consecutiveFailures: number
  latency: number // ms
  circuitState?: CircuitState
  circuitBrokenAt?: string
  nextRetryAt?: string
  halfOpenSuccesses?: number
  breakerFailureRate?: number
  lastSuccessAt?: string
  lastFailureAt?: string
  // 最近 24 小时按 ConsumptionPolicy 聚合的请求分布
  consumptionPolicyDistribution?: Record<string, number>
  // 分时段统计 (15m, 1h, 6h, 24h)
  timeWindows?: {
    '15m': TimeWindowStats
    '1h': TimeWindowStats
    '6h': TimeWindowStats
    '24h': TimeWindowStats
  }
}

export interface SchedulerStatsResponse {
  multiChannelMode: boolean
  activeChannelCount: number
  traceAffinityCount: number
  traceAffinityTTL: string
  failureThreshold: number
  windowSize: number
  circuitRecoveryTime?: string
  consecutiveRetryableFailuresThreshold?: number
  halfOpenSuccessTarget?: number
  circuitBackoffBase?: string
  circuitBackoffMax?: string
}

// 历史数据点（用于时间序列图表）
export interface HistoryDataPoint {
  timestamp: string
  requestCount: number
  successCount: number
  failureCount: number
  successRate: number
  inputTokens?: number
  outputTokens?: number
  cacheCreationTokens?: number
  cacheReadTokens?: number
}

// 渠道历史指标响应
export interface MetricsHistoryResponse {
  channelIndex: number
  channelName: string
  dataPoints: HistoryDataPoint[]
  summary?: GlobalStatsSummary
}

// Key 级别历史数据点（包含 Token 数据）
export interface KeyHistoryDataPoint {
  timestamp: string
  requestCount: number
  successCount: number
  failureCount: number
  successRate: number
  inputTokens: number
  outputTokens: number
  cacheCreationTokens: number
  cacheReadTokens: number
  costUSD?: number
}

// 单个 Key 的历史数据
export interface KeyHistoryData {
  keyIdentity?: string // 稳定单向标识，用于同一逻辑渠道跨协议合并
  keyMask: string
  model?: string // 模型名（可选，用于 Key+Model 组合显示）
  color: string
  dataPoints: KeyHistoryDataPoint[]
}

// 渠道 Key 级别历史指标响应
export interface ChannelKeyMetricsHistoryResponse {
  channelIndex: number
  channelName: string
  keys: KeyHistoryData[]
  summary?: GlobalStatsSummary
}

// ============== 全局统计类型 ==============

// 全局历史数据点（包含 Token 数据）
export interface GlobalHistoryDataPoint {
  timestamp: string
  requestCount: number
  successCount: number
  failureCount: number
  successRate: number
  inputTokens: number
  outputTokens: number
  cacheCreationTokens: number
  cacheReadTokens: number
  costUSD?: number
}

// 全局统计汇总
export interface GlobalStatsSummary {
  totalRequests: number
  totalSuccess: number
  totalFailure: number
  totalInputTokens: number
  totalOutputTokens: number
  totalCacheCreationTokens: number
  totalCacheReadTokens: number
  totalCostUSD?: number
  avgSuccessRate: number
  duration: string
  intervalSeconds?: number
}

// 全局统计响应
export interface GlobalStatsHistoryResponse {
  dataPoints: GlobalHistoryDataPoint[]
  summary: GlobalStatsSummary
  modelDataPoints?: Record<string, ModelHistoryDataPoint[]>
}

// ============== 模型统计类型 ==============

export interface ModelHistoryDataPoint {
  timestamp: string
  requestCount: number
  successCount: number
  failureCount: number
  inputTokens: number
  outputTokens: number
  cacheCreationTokens: number
  cacheReadTokens: number
  costUSD?: number
}

export interface ModelStatsHistoryResponse {
  models: Record<string, ModelHistoryDataPoint[]>
  duration: string
  interval: string
}

// ============== 渠道日志类型 ==============

export interface ChannelLogEntry {
  requestId: string
  timestamp: string
  model: string
  originalModel?: string
  operation?: string
  originalReasoningEffort?: string
  actualReasoningEffort?: string
  statusCode: number
  durationMs: number
  success: boolean
  keyMask: string
  baseUrl: string
  errorInfo: string
  isRetry: boolean
  interfaceType?: string // 接口类型（Messages/Responses/Gemini）
  requestSource?: string
  selectionReason?: string
  selectionTraceSummary?: string
  racingRole?: string
  racingStatus?: string

  // 请求生命周期状态
  status: string // pending/connecting/first_byte/streaming/completed/failed/cancelled/racing_lost
  startTime: string
  connectedAt?: string
  firstByteAt?: string
  completedAt?: string
  firstContentLatencyMs?: number
  maxStreamIdleMs?: number
  maxToolCallIdleMs?: number

  // 代理上下文观测（subagent 识别）
  agentRole?: string // main | subagent
  agentType?: string // codex_subagent | claude_code_subagent
  parentThreadId?: string // Codex parent thread id
  agentConfidence?: string // exact | heuristic
  sessionId?: string // 扁平化会话标识（用于驾驶舱关联）

  // Autopilot trace 关联
  autopilotTraceUid?: string
  requestCorrelationId?: string
}

// 熔断成因依据：渠道日志是后端内存态，重启即清空，而熔断状态持久化恢复。
// 日志为空且渠道非 closed 时后端返回该字段，用于解释熔断来源而非留下黑盒。
export interface ChannelBreakerEvidence {
  circuitState: 'open' | 'half_open' | 'closed'
  lastFailureAt?: string
  circuitBrokenAt?: string
  nextRetryAt?: string
  backoffLevel: number
  consecutiveFailures: number
  // true 表示熔断依据的最近失败早于本次进程启动，即日志已随重启清空
  predatesRestart: boolean
  processStartedAt: string
}

export interface ChannelLogsResponse {
  channelIndex: number
  logs: ChannelLogEntry[]
  breakerEvidence?: ChannelBreakerEvidence
}

// ============== 渠道实时活跃度类型 ==============

// 活跃度分段数据（每 6 秒一段）
export interface ActivitySegment {
  requestCount: number
  successCount: number
  failureCount: number
  inputTokens: number
  outputTokens: number
}

// 渠道最近活跃度数据（稀疏格式，减少 JSON 体积）
export interface ChannelRecentActivity {
  channelIndex: number
  routeKind?: ChannelKind
  segments: Record<number, ActivitySegment> | ActivitySegment[] // 稀疏 Map 或数组格式（兼容旧版）
  totalSegs: number // 总段数（固定 150）
  rpm: number // 15分钟平均 RPM
  tpm: number // 15分钟平均 TPM
}
