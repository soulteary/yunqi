import type { ChannelKind } from './common'

export interface SchedulerDiagnoseContextRequirement {
  inputTokens?: number
  outputTokens?: number
  requiredTokens?: number
  minimumContextWindowTokens?: number
  explicitOutputMax?: boolean
  skipWindowValidation?: boolean
}

export interface SchedulerDiagnoseRequest {
  userId?: string
  model?: string
  routePrefix?: string
  channelName?: string
  failedChannels?: number[]
  hasImageContent?: boolean
  agentRole?: string
  contextRequirement?: SchedulerDiagnoseContextRequirement
}

export interface SchedulerTraceStage {
  name: string
  count: number
}

export interface SchedulerTraceCandidate {
  channelIndex: number
  channelName: string
  stage: string
  reason: string
  details?: string
}

export interface SchedulerTraceSelection {
  channelIndex: number
  channelName: string
  reason: string
}

export interface SchedulerSelectionTrace {
  kind: ChannelKind
  model?: string
  routePrefix?: string
  channelName?: string
  agentRole?: string
  stages?: SchedulerTraceStage[]
  candidates?: SchedulerTraceCandidate[]
  selected?: SchedulerTraceSelection
}

export interface SchedulerDiagnoseResponse {
  ok: boolean
  kind: ChannelKind
  reason?: string
  summary?: string
  error?: string
  selected?: {
    channelIndex: number
    channelName: string
    serviceType?: string
  }
  trace?: SchedulerSelectionTrace
}

// ============== 驾驶舱类型 ==============

export interface CockpitHealthSummary {
  totalChannels: number
  totalEndpoints: number
  stateCounts: Record<string, number>
}

export interface CockpitSubscriptionSummary {
  total: number
  balanceByCode: Record<string, number>
  countByMode: Record<string, number>
  countByTier: Record<string, number>
}

export interface CockpitLocalRuntimeSummary {
  total: number
  statusCounts: Record<string, number>
  totalModels: number
}

export interface CockpitManualIntentSummary {
  activeCount: number
  totalCount: number
}

export interface CockpitTodoItem {
  endpointUid: string
  channelUid: string
  channelKind: string
  baseUrl: string
  healthState: string
  suggestedAction: string
}

export interface CockpitOverviewResponse {
  health: CockpitHealthSummary
  subscriptions: CockpitSubscriptionSummary
  localRuntimes: CockpitLocalRuntimeSummary
  manualIntents: CockpitManualIntentSummary
  todoItems: CockpitTodoItem[]
}

// ============== 渠道推荐类型（Phase 4 Item 4）==============

export interface ChannelRecommendation {
  proxyKeyMask: string
  domain: string
  domainUsageCount: number
  currentChannelUid: string
  currentScore: number
  recommendedChannelUid: string
  recommendedScore: number
  scoreDelta: number
  reason: string
}

export interface RecommendationsResponse {
  proxyKeyMask?: string
  recommendations: ChannelRecommendation[]
}

// ============== 人工路由意图（试用意图）类型 ==============

/** 人工路由意图的类型 */
export type ManualIntentType = 'model_trial' | 'channel_trial' | 'endpoint_trial' | 'session_pin'

/** 人工路由意图的生命周期状态 */
export type ManualIntentStatus = 'active' | 'expired' | 'exhausted' | 'disabled'

/** 意图作用范围的任务类别 */
export type ManualIntentTaskClass =
  | 'supervisor'
  | 'worker'
  | 'lightweight'
  | 'vision'
  | 'long_context'
  | 'image_generation'
  | 'embedding'

/** 试用结果统计（Phase 1 shadow：仅记录统计，不影响真实调度） */
export interface TrialResult {
  hitCount: number
  successCount: number
  failureCount: number
  totalLatencyMs?: number
  avgLatencyMs: number
  fallbackCount?: number
  estimatedCost?: number
}

/** POST /manual-intents 请求体 */
export interface CreateIntentRequest {
  name?: string
  intentType: ManualIntentType
  channelKind: string
  channelUid?: string
  metricsKey?: string
  model?: string
  mappedModel?: string
  agentRoles?: string[]
  taskClasses?: ManualIntentTaskClass[]
  sessionId?: string
  trafficPercent?: number
  expiresAt?: string
  ttlMinutes?: number
  maxRequests?: number
  maxEstimatedCost?: number
  fallbackOnFailure?: boolean
  requireHardConstraints: boolean
  createdBy?: string
  reason?: string
}

/** 人工路由意图（试用意图）完整记录 */
export interface ManualRoutingIntent {
  intentUid: string
  name?: string
  intentType: ManualIntentType
  channelKind: string
  channelUid?: string
  metricsKey?: string
  model?: string
  mappedModel?: string
  agentRoles?: string[]
  taskClasses?: ManualIntentTaskClass[]
  sessionId?: string
  trafficPercent?: number
  expiresAt: string
  maxRequests?: number
  maxEstimatedCost?: number
  fallbackOnFailure?: boolean
  requireHardConstraints: boolean
  createdBy?: string
  createdAt: string
  reason?: string
  status: ManualIntentStatus
  trialResult: TrialResult
}

/** GET /manual-intents 列表响应 */
export interface IntentListResponse {
  intents: ManualRoutingIntent[]
  total: number
}

// ============== Autopilot 智能路由类型 ==============

export interface SmartRoutingCostPreference {
  mode: 'quality_first' | 'balanced' | 'cost_first' | 'custom'
}

export interface ScenarioPresetView {
  key: string
  minQualityTier: string
  costPreference: string
  effortFloor?: string
  effortCeil?: string
  qualityBenefitCap?: string
}

export type RoutingScenario = 'auto' | 'daily_dev' | 'hard_problem' | 'background' | 'batch_cheap'

export interface SmartRoutingConfig {
  killSwitchActive: boolean
  costPreference: string
  scenario?: RoutingScenario
  scenarioPresets?: ScenarioPresetView[]
  l2ProbeEnabled?: boolean
  racingEnabled?: boolean
}

export interface CandidateScore {
  dimension: string
  score: number
  weight: number
}

export interface DomainStrengthEvidence {
  source: 'endpoint_override' | 'canonical_benchmark' | 'family_seed' | 'neutral'
  score: number
  canonicalCeiling?: number
  providerQualityFactor?: number
  canonicalModel?: string
  benchmarkCategory?: string
  benchmarkSources?: string[]
  benchmarkVerifiedAt?: string
  benchmarkLane?: string
  evidenceConfidence?: number
}

export interface RoutingCandidate {
  channelUid: string
  channelName?: string
  candidateKey?: string // 五元组标识（v3）：channelUID|protocol|keyIdentity|model|effort；v2 前为二元组 channelUID|model
  metricsKey?: string
  keyMask?: string
  originTier?: string
  channelKind?: string
  executionKind?: string
  protocolFidelity?: string
  conversionPenalty?: number
  healthState?: string
  mappedModel?: string
  mappingSource?: string
  mappingReason?: string
  actualModel?: string // v3：候选行实际发送模型（五元组模型维）
  keyIdentity?: string // v3：候选行 key 身份（KeyUID 或 kh_ 哈希前缀）
  quotaGroup?: string // v3：key 分组
  effort?: string // v3：思考档位（空 = passthrough）
  baseQualityTier?: string
  effortQualityTier?: string
  effortQualityScore?: number
  effortEvidenceClass?: string
  effortQualityKnown?: boolean
  effortAwareTotalScore?: number
  qualityConfidence?: number
  qualityDiscount?: number
  qualityDiscountReason?: string
  totalScore: number
  scores?: CandidateScore[]
  penalty?: number
  domainEvidence?: DomainStrengthEvidence
  selected: boolean
  filterReasons?: string[]
}

// ── A/B 测试（Phase 4 Item 8） ──

export interface ABTestRecord {
  recordUid: string
  model: string
  channelKind: string
  primaryChannelUid: string
  primarySuccess: boolean
  primaryStatusCode: number
  primaryLatencyMs: number
  shadowChannelUid: string
  shadowSuccess: boolean
  shadowStatusCode: number
  shadowLatencyMs: number
  shadowError?: string
  shadowCostUsd: number
  traceUid?: string
  createdAt: string
}

export interface ABTestChannelStats {
  channelUid: string
  count: number
  successCount: number
  successRate: number
  avgLatencyMs: number
  totalCostUsd: number
}

export interface ABTestStats {
  totalRecords: number
  shadowSuccessCount: number
  shadowFailCount: number
  shadowSuccessRate: number
  avgShadowLatencyMs: number
  totalShadowCostUsd: number
  byChannel?: Record<string, ABTestChannelStats>
}

export interface ABTestResultsResponse {
  enabled: boolean
  sampleRatio: number
  shadowCandidateCount: number
  budgetUsed: number
  budgetRemaining: number
  maxBudgetPerHour: number
  killSwitchActive: boolean
  stats: ABTestStats
  recentRecords: ABTestRecord[]
  totalShadowCostUsd: number
}

export interface ABTestEmergencyStopResponse {
  ok: boolean
  action: string
  reason: string
  note: string
}
