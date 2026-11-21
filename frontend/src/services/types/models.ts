import type { Channel } from './channels'
import type { ChannelAuthHeader } from './common'

export interface UpstreamModelCapability {
  contextWindowTokens?: number
  maxOutputTokens?: number
  defaultOutputTokens?: number
  recommendedOutputTokens?: number
  thinkingMode?: string
  reasoningEfforts?: string[]
  provider?: string
  displayName?: string
  description?: string
  capabilities?: Record<string, boolean>
  pricing?: ModelPricing
  sources?: string[]
  /** 厂商文档已知的请求参数硬约束（如 Kimi 固定值采样参数），只读展示用，不参与前端逻辑。 */
  paramConstraints?: ModelParamConstraints
}

export interface ModelParamConstraints {
  fixedParams?: string[]
  toolChoiceRequiredUnsupported?: boolean
  thinkingFixedValue?: Record<string, unknown>
}

export interface ModelBenchmarkProfile {
  canonicalModel: string
  overallScore?: number
  categoryScores?: Record<string, number>
  benchmarkEvidence?: ModelBenchmarkEvidence[]
  sources?: string[]
  verifiedAt?: string
  lane?: 'provisional' | 'verified'
  sharedResults?: number
  comparableCategories?: number
  totalCategories?: number
}

export interface ModelBenchmarkEvidence {
  benchmark: string
  benchmarkVersion: string
  sourceModel: string
  domain: string
  metric: string
  rawValue: number
  uncertainty?: number
  cohortPercentile: number
  taskCount: number
  cohortSize: number
  effort: string
  selectionBasis: string
  sourceUrl: string
  capturedAt: string
}

export interface EmbeddingCapability {
  embeddingSpaceId?: string
  dimensions?: number
  supportedDimensions?: number[]
  normalized?: boolean
}

export interface ModelPricing {
  unit?: string
  currency?: string
  inputCacheHitPrice?: number
  inputCacheMissPrice?: number
  outputPrice?: number
  tiers?: ModelPricingTier[]
}

export interface ModelPricingTier {
  label?: string
  inputTokensAbove?: number
  inputTokensUpTo?: number
  inputCacheHitPrice?: number
  inputCacheMissPrice?: number
  outputPrice?: number
}

export interface ModelEntry {
  id: string
  object: string
  created: number
  owned_by: string
}

export interface ModelsResponse {
  object: string
  data: ModelEntry[]
  /** 上游真实状态码（新契约字段）；旧后端无此字段，调用方按 200 兜底 */
  statusCode?: number
}

export interface ChannelModelsRequest {
  key: string
  baseUrl?: string
  serviceType?: Channel['serviceType']
  // 已学习客户端指纹校验的上游：探测请求需带 Claude Code 伪装头，
  // 否则编辑器 per-key 校验会被指纹拦截误报「上游 API Key 无效」。
  learnedClientFingerprint?: boolean
  proxyUrl?: string
  proxyPreferDirect?: boolean
  insecureSkipVerify?: boolean
  customHeaders?: Record<string, string>
  authHeader?: ChannelAuthHeader | ''
  baseUrls?: string[]
}
