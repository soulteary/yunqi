import type { RoutingCandidate } from './routing'

export interface RoutingDecisionTrace {
  traceUid: string
  requestKind: string
  taskClass: string
  taskDomain?: string
  requestedModel?: string
  agentRole?: string
  candidates: RoutingCandidate[]
  candidatesBefore: number
  candidatesAfter: number
  globalFilterReasons?: Record<string, string[]>
  sortReasons?: string[]
  selectedChannelUid?: string
  selectedMetricsKey?: string
  selectedOriginTier?: string
  estimatedCost?: number
  costConfidence?: number
  fallbackUsed: boolean
  shadowChannelUid?: string
  actualChannelUid?: string
  match: boolean
  outcomeRecorded?: boolean
  outcome?: 'success' | 'upstream_error' | 'exhausted' | 'cancelled' | 'attempt_failed'
  success?: boolean
  channelFallback?: boolean
  statusCode?: number
  requestDurationMs?: number
  firstByteLatencyMs?: number
  completedAt?: string
  mode: 'off' | 'shadow' | 'assist' | 'auto' | 'active' | 'dry_run'
  durationMs: number
  createdAt: string
}

export interface AutopilotTraceListResponse {
  traces: TraceSummary[]
  total: number
  partial?: boolean
  hasMore: boolean
}

export interface AutopilotTraceStats {
  totalCount: number
  comparedCount: number
  matchedCount?: number
  mismatchCount: number
  uncomparedCount?: number
  successCount?: number
  failOpenCount?: number
  mismatchRate: number
  taskClassDist: Record<string, number>
  modeDist: Record<string, number>
}

// ── Trace v2 类型 ──

export type ComparisonStatus = 'matched' | 'mismatched' | 'uncompared'

export type Cohort = 'treatment' | 'control' | 'bypass'

/** 列表摘要（v2） */
export interface TraceSummary {
  traceUid: string
  schemaVersion: number
  createdAt: string
  releaseId?: string
  cohort?: Cohort
  mode: string
  requestKind: string
  taskClass: string
  taskDomain?: string
  requestedModel?: string
  actualModel?: string
  actualEffort?: string
  comparisonStatus: ComparisonStatus
  recommendedChannelUid?: string
  actualChannelUid?: string
  outcome?: string
  success?: boolean
  requestDurationMs?: number
  historicalSchema?: boolean
}

/** Scheduler 裁决摘要 */
export interface SchedulerDecisionSummary {
  stages?: { name: string; count: number }[]
  skipReasons?: string[]
  skippedCandidates?: SkippedCandidateSummary[]
  selectedUid?: string
  selectedName?: string
  selectionCode?: string
}

/** scheduler 阶段被过滤渠道的明细 */
export interface SkippedCandidateSummary {
  channelIndex: number
  channelName: string
  stage: string
  reason: string
  details?: string
}

/** endpoint 尝试摘要 */
export interface EndpointAttemptSummary {
  attemptUid: string
  attemptSeq: number
  status: string
  channelUid: string
  endpointLabel: string
  actualModel?: string
  actualEffort?: string
  result: string
  statusCode?: number
  durationMs?: number
}

/** trace 详情（v2） */
export interface TraceDetailV2 {
  traceUid: string
  schemaVersion: number
  traceRevision?: number
  createdAt: string
  requestCorrelationId?: string
  source?: string
  releaseId?: string
  policyFingerprint?: string
  targetMode?: string
  effectiveMode?: string
  cohort?: Cohort
  bypassReason?: string
  persistenceClass?: string
  comparisonStatus: ComparisonStatus
  requestKind: string
  taskClass: string
  taskDomain?: string
  requestedModel?: string
  actualModel?: string
  actualEffort?: string
  agentRole?: string
  manualIntentUid?: string
  advisorDecisionUid?: string
  candidates?: RoutingCandidate[]
  candidatesBefore: number
  candidatesAfter: number
  globalFilterReasons?: Record<string, string[]>
  sortReasons?: string[]
  recommendedChannelUid?: string
  selectedChannelUid?: string
  estimatedCost?: number
  costConfidence?: number
  fallbackUsed: boolean
  schedulerDecision?: SchedulerDecisionSummary
  endpointAttempts?: EndpointAttemptSummary[]
  attemptsTruncated?: boolean
  attemptsTotal?: number
  attemptsByResult?: Record<string, number>
  outcome?: string
  success?: boolean
  channelFallback?: boolean
  statusCode?: number
  requestDurationMs?: number
  firstByteLatencyMs?: number
  completedAt?: string
  durationMs: number
  historicalSchema?: boolean
}

export interface AutopilotTraceDetailResponse {
  trace: TraceDetailV2
}
