import type { Channel } from './channels'
import type { ChannelAuthHeader } from './common'

// ============== 能力测试类型 ==============

export interface CapabilityProtocolJobRef {
  jobId: string
  channelKind: 'messages' | 'chat' | 'gemini' | 'responses'
  channelId: number
}

export interface CapabilityTestJobStartResponse {
  jobId: string
  resumed?: boolean
  job?: CapabilityTestJob
}

export interface StartCapabilityTestOptions {
  targetProtocols?: string[]
  previousJobId?: string
  rpm?: number
  sourceTab?: string
  models?: string[]
  useChannelModels?: boolean // 以渠道认可的模型列表（上游清单/管控面）为探测范围
}

export type CapabilityLifecycle = 'pending' | 'active' | 'done' | 'cancelled'

export type CapabilityOutcome = 'unknown' | 'success' | 'failed' | 'partial' | 'cancelled'

export type CapabilityRunMode =
  | 'fresh'
  | 'reused_running'
  | 'resumed_cancelled'
  | 'cache_hit'
  | 'reused_previous_results'

export type CapabilityTestJobStatus = 'idle' | 'queued' | 'running' | 'completed' | 'failed' | 'cancelled'

export type CapabilityProtocolJobStatus = 'idle' | 'queued' | 'running' | 'completed' | 'failed'

export type CapabilityModelJobStatus = 'idle' | 'queued' | 'running' | 'success' | 'failed' | 'skipped'

export type ImageGenerationProbeState = 'supported' | 'unsupported' | 'inconclusive'

export interface CodexImageGenerationKeyProbeResult {
  keyMask: string
  hostedTool: ImageGenerationProbeState
  namespaceTool: ImageGenerationProbeState
  status: ImageGenerationProbeState
}

export interface CodexImageGenerationProbeSummary {
  tested: boolean
  supported: boolean
  compatibleViaStrip?: boolean
  actualModel: string
  supportedKeys: number
  unsupportedKeys: number
  inconclusiveKeys: number
  keyResults?: CodexImageGenerationKeyProbeResult[]
}

export interface CapabilityJobProgress {
  totalModels: number
  queuedModels: number
  runningModels: number
  successModels: number
  failedModels: number
  skippedModels: number
  completedModels: number
}

export interface CapabilityModelJobResult {
  model: string
  actualModel?: string // 实际使用的模型名（现仅 Codex 图片探测回填）
  upstreamModel?: string // 上游响应自报的模型名（识别厂商侧隐式重定向）
  status: CapabilityModelJobStatus
  lifecycle: CapabilityLifecycle
  outcome: CapabilityOutcome
  reason?: string
  success: boolean
  latency: number
  streamingSupported: boolean
  codexImageGeneration?: CodexImageGenerationProbeSummary
  error?: string
  startedAt?: string
  testedAt?: string
}

export interface CapabilityProtocolJobResult {
  protocol: string
  status: CapabilityProtocolJobStatus
  lifecycle: CapabilityLifecycle
  outcome: CapabilityOutcome
  reason?: string
  success: boolean
  latency: number
  streamingSupported: boolean
  testedModel: string
  modelResults?: CapabilityModelJobResult[]
  successCount?: number
  attemptedModels?: number
  error?: string
  testedAt: string
}

export interface CapabilityTestJob {
  jobId: string
  protocolJobIds?: Record<string, string>
  protocolJobRefs?: Record<string, CapabilityProtocolJobRef>
  channelId: number
  channelName: string
  channelKind: string
  sourceType: string
  status: CapabilityTestJobStatus
  lifecycle: CapabilityLifecycle
  outcome: CapabilityOutcome
  reason?: string
  runMode?: CapabilityRunMode
  summaryReason?: string
  activeOperations?: number
  isResumed?: boolean
  hasReusedResults?: boolean
  tests: CapabilityProtocolJobResult[]
  redirectTests?: RedirectModelResult[]
  compatibleProtocols: string[]
  totalDuration: number
  startedAt?: string
  updatedAt: string
  finishedAt?: string
  progress: CapabilityJobProgress
  error?: string
  cacheHit?: boolean
  targetProtocols?: string[]
  timeoutMilliseconds?: number
  schemaVersion?: number
  snapshotUpdatedAt?: string
}

// RedirectModelResult 单个探测模型的实测结果（现仅 Codex 图片探测回填 actualModel）
export interface RedirectModelResult {
  probeModel: string // 原生探测模型名
  actualModel: string // 实际发给上游的模型名
  upstreamModel?: string // 上游响应自报的模型名（识别厂商侧隐式重定向）
  success: boolean
  latency: number
  streamingSupported?: boolean
  codexImageGeneration?: CodexImageGenerationProbeSummary
  error?: string
  startedAt?: string
  testedAt: string
}

export interface CapabilitySnapshot {
  identityKey: string
  sourceType: string
  protocolJobIds?: Record<string, string>
  protocolJobRefs?: Record<string, CapabilityProtocolJobRef>
  tests: CapabilityProtocolJobResult[]
  compatibleProtocols: string[]
  totalDuration: number
  progress: CapabilityJobProgress
  lifecycle: CapabilityLifecycle
  outcome: CapabilityOutcome
  updatedAt: string
}

export interface ModelTestResult {
  model: string
  actualModel?: string
  upstreamModel?: string // 上游响应自报的模型名（识别厂商侧隐式重定向）
  success: boolean
  latency: number
  streamingSupported: boolean
  codexImageGeneration?: CodexImageGenerationProbeSummary
  error?: string
  startedAt?: string
  testedAt: string
}

export interface ProtocolTestResult {
  protocol: string
  success: boolean
  latency: number
  streamingSupported: boolean
  testedModel: string
  modelResults?: ModelTestResult[]
  successCount?: number
  attemptedModels?: number
  error?: string
  testedAt: string
}

export interface CapabilityTestResult {
  channelId: number
  channelName: string
  sourceType: string
  tests: ProtocolTestResult[]
  compatibleProtocols: string[]
  totalDuration: number
  schemaVersion?: number
}

export interface CompatDiagnoseResult {
  recommendations: Partial<Record<string, boolean>>
  urlRecommendations?: {
    current: string
    recommended: string
    reason: string
  }
  evidence: Partial<Record<string, string>>
  duration: number
  cached: boolean
}

export type ChannelDiscoveryKind = 'messages' | 'chat' | 'gemini' | 'responses'

export type ChannelDiscoveryTargetClient = 'codex' | 'claude-code' | 'claude'

export interface ChannelDiscoveryRequest {
  channelKind?: ChannelDiscoveryKind
  serviceType?: Channel['serviceType'] | ''
  baseUrl?: string
  baseUrls?: string[]
  apiKey: string
  authHeader?: ChannelAuthHeader | ''
  customHeaders?: Record<string, string>
  proxyUrl?: string
  proxyPreferDirect?: boolean
  insecureSkipVerify?: boolean
  targetClients?: ChannelDiscoveryTargetClient[]
}

export interface DiscoverySelectedModels {
  strong?: string
  primary?: string
  fast?: string
}

export interface DiscoveryModelsResult {
  source: string
  url?: string
  statusCode?: number
  items: string[]
  selected: DiscoverySelectedModels
  warnings?: string[]
}

export interface DiscoveryProtocolResult {
  protocol: ChannelDiscoveryKind
  success: boolean
  successModels?: string[]
  failedModels?: string[]
  latencyMs?: number
  error?: string
}

export interface DiscoveryCapabilityProbeResult {
  tested: boolean
  supported: boolean
  required?: boolean
  statusCode?: number
  evidence?: string
  error?: string
  recommendation?: Partial<Record<string, boolean>>
}

export interface DiscoveryCapabilitiesResult {
  toolCalls: DiscoveryCapabilityProbeResult
  vision: DiscoveryCapabilityProbeResult
  imageGeneration: DiscoveryCapabilityProbeResult
  thinkingPassback: DiscoveryCapabilityProbeResult
}

export interface DiscoveryRateLimitResult {
  initialRpm: number
  effectiveRpm: number
  rateLimited: boolean
  rateLimitedCount?: number
}

export interface DiscoveryEvidence {
  type: string
  key?: string
  message: string
}

export interface ChannelDiscoveryRecommendation {
  channelKind: ChannelDiscoveryKind | ''
  serviceType: Channel['serviceType'] | ''
  baseUrls?: string[]
  supportedModels?: string[]
  compat?: Partial<Record<string, boolean>>
  urlRecommendation?: {
    current: string
    recommended: string
    reason: string
  } | null
  evidence?: DiscoveryEvidence[]
}

export interface ChannelDiscoveryResponse {
  models: DiscoveryModelsResult
  protocols: DiscoveryProtocolResult[]
  capabilities: DiscoveryCapabilitiesResult
  recommendation: ChannelDiscoveryRecommendation
  rateLimit: DiscoveryRateLimitResult
  evidence?: DiscoveryEvidence[]
}

/** 快速探活请求：仅探一个真实模型以定 primaryKind，不做全量协议/能力探测。 */
export interface ChannelDiscoveryFastRequest {
  /** 可选提示，不能限制自动探测结果 */
  channelKind?: ChannelDiscoveryKind
  baseUrl?: string
  baseUrls?: string[]
  /** 兼容单个 key */
  apiKey?: string
  apiKeys?: string[]
  authHeader?: ChannelAuthHeader | ''
  customHeaders?: Record<string, string>
  proxyUrl?: string
  proxyPreferDirect?: boolean
  insecureSkipVerify?: boolean
}

/** 快速探活响应：primaryKind 必须来自成功协议；testedModel/streamingSupported 仅作证据，不写入渠道白名单。 */
export interface ChannelDiscoveryFastResponse {
  primaryKind: ChannelDiscoveryKind | ''
  testedModel: string
  streamingSupported: boolean
  testedKeyHash: string
  rateLimit: DiscoveryRateLimitResult
}
