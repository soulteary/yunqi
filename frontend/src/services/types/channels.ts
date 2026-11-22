import type { OutboundNetworkConfig } from './network'
import type { ChannelAuthHeader, ChannelDisplayStatus, ChannelKind } from './common'
import type { ChannelBalanceCheckConfig } from './health'
import type { ChannelMetrics, ChannelRecentActivity, SchedulerStatsResponse } from './metrics'
import type { EmbeddingCapability, UpstreamModelCapability } from './models'

export interface DisabledKeyInfo {
  key: string
  reason: string // "authentication_error" / "permission_error" / "insufficient_balance" / "insufficient_quota"
  message: string
  disabledAt: string // ISO8601 时间戳
  recoverAt?: string // 自动恢复时间（可选，ISO8601）
  config?: APIKeyConfig // 拉黑前的 key 配置快照，restore 时恢复
}

// 被限制的 (Key, 模型) 组合（model_not_found 等场景，仅限制该 Key 对该模型的路由）
export interface DisabledKeyModelInfo {
  key: string
  model: string // 触发限制的模型
  reason: string // "model_not_found"
  message: string
  disabledAt: string // ISO8601 时间戳
  recoverAt: string // 自动恢复时间（ISO8601）
}

// 人工设置的配额分组模型禁用，与 disabledKeyModels 的自动临时限制相互独立。
export interface DisabledGroupModelInfo {
  quotaGroup: string
  key?: string
  model: string
  disabledAt: string
}

export interface GroupModelPolicyResponse {
  success: boolean
  quotaGroup: string
  model: string
  affectedKeyCount: number
}

export interface APIKeyConfig {
  /** 新建配置通常按 key 关联；服务端身份骨架可将 key 留空并仅返回 keyUid。 */
  key: string
  keyUid?: string
  credentialUid?: string
  name?: string
  baseUrl?: string
  enabled?: boolean
  quotaGroup?: string
  groupMultiplier?: number | null
  maxGroupMultiplier?: number | null
  multiplierSource?: 'manual' | 'new_api' | 'provider'
  consumptionPolicy?: 'normal' | 'opportunistic'
  effectiveCostClass?: 'zero' | 'discounted' | 'standard' | 'premium' | 'unknown'
  multiplierUpdatedAt?: string
  multiplierExpiresAt?: string
  multiplierSyncStatus?: 'manual' | 'fresh' | 'stale' | 'over_limit' | 'sync_error' | 'relink_required' | string
  multiplierSyncError?: string
  sourceSubscriptionUid?: string
  sourceRemoteTokenId?: number
  eligible?: boolean
  ineligibleReason?: string
  rateLimitRpm?: number
  rateLimitWindowMinutes?: number
  rateLimitMaxConcurrent?: number
  rateLimitAutoFromHeaders?: boolean
  weight?: number
  models?: string[]
  /** 保留服务端未来扩展字段，整对象编辑时不得意外丢失。 */
  [key: string]: unknown
}

export interface Channel {
  name: string
  accountUid?: string // 自动托管账号稳定身份，同一 provider 的多协议渠道共享
  channelUid?: string // 渠道稳定身份标识（创建后不因重排/改名/Key 变更而改变）
  logicalChannelUid?: string // 逻辑渠道 UID（同站点多协议渠道共享；后端 RebuildLogicalChannels 回填）
  providerId?: string // 已知 provider 模板 ID（如 mimo/deepseek）
  routeKind?: ChannelKind // 前端统一列表中真实所属的后端渠道类型
  routeIndex?: number // 前端统一列表中真实后端渠道索引
  logicalName?: string // 前端统一列表中的逻辑渠道名称
  displayKey?: string // 前端统一列表中的稳定展示 key
  protocolCapsules?: ChannelProtocolCapsule[]
  protocolRoutes?: ChannelProtocolRoute[]
  serviceType: 'openai' | 'gemini' | 'claude' | 'responses' | 'copilot'
  authHeader?: ChannelAuthHeader | ''
  baseUrl: string
  baseUrls?: string[] // 多 BaseURL 支持（failover 模式）
  apiKeys: string[]
  apiKeyConfigs?: APIKeyConfig[]
  disabledApiKeys?: DisabledKeyInfo[] // 被拉黑的 API Key
  disabledKeyModels?: DisabledKeyModelInfo[] // 自动临时限制的 (Key, 模型) 组合
  disabledGroupModels?: DisabledGroupModelInfo[] // 人工禁用的 (配额分组, 模型) 组合
  historicalApiKeys?: string[]
  description?: string
  remark?: string
  website?: string
  insecureSkipVerify?: boolean
  claudeDesktopModelTiers?: Record<string, string>
  claudeDesktopTierRules?: Readonly<Record<string, string>>
  modelCapabilities?: Record<string, UpstreamModelCapability>
  embeddingCapabilities?: Record<string, EmbeddingCapability>
  defaultCapability?: UpstreamModelCapability
  allowUnknownContext?: boolean
  reasoningParamStyle?: 'reasoning' | 'reasoning_effort' | 'thinking'
  textVerbosity?: 'low' | 'medium' | 'high' | ''
  fastMode?: boolean
  customHeaders?: Record<string, string> // 自定义请求头
  proxyUrl?: string // HTTP/HTTPS/SOCKS5 代理 URL
  outboundNetwork?: OutboundNetworkConfig | null
  proxyPreferDirect?: boolean // 直连优先：配代理时先直连，失败（网络错误/451/403）自动回退代理
  costMultiplier?: number // 渠道级充值倍率（EffectiveCost = ListCost × 倍率，0/空=不参与）
  maxGroupMultiplier?: number // 渠道级最高分组倍率上限（Key 分组倍率超过则自动退出调度，0/空=不启用闸门）
  channelPaymentCurrency?: string // 充值币种（如 LDC/CNY/USD）
  channelPaymentAmount?: number // 充值金额（0/空=不参与）
  channelCreditCurrency?: string // 渠道显示/计价币种（如 USD）
  channelCreditAmount?: number // 渠道到账金额（0/空=不参与）
  requestTimeoutMs?: number // 非流式上游请求超时时间（毫秒，0/空=继承全局）
  responseHeaderTimeoutMs?: number // 等待上游 HTTP 响应头超时时间（毫秒，0/空=继承全局）
  streamFirstContentTimeoutMs?: number // 流式首字等待超时（毫秒，0/空=继承全局）
  streamInactivityTimeoutMs?: number // 流式首字后断流超时（毫秒，0/空=继承全局）
  streamToolCallIdleTimeoutMs?: number // 工具调用空闲超时（毫秒，0/空=继承全局）
  routePrefix?: string // 路由前缀（如 "kimi"，访问 /kimi/v1/messages）
  autoBlacklistBalance?: boolean // 余额不足自动拉黑（默认 true）
  normalizeMetadataUserId?: boolean // 规范化 metadata.user_id（默认 true）
  stripBillingHeader?: boolean // Messages 渠道：移除 billing header（默认：非官方 Anthropic 域名 true，官方域名 false）
  stripEmptyTextBlocks?: boolean // Claude 协议特定：转发前移除裸空 text content block（兼容严格校验的第三方上游）
  normalizeSystemRoleToTopLevel?: boolean // Claude 协议特定：将 messages 中 system 角色抽取回顶层 system 字段（兼容仅支持 user/assistant 的旧上游）
  responsesReasoningFold?: boolean // Responses 推理自动续接（默认 false）
  codexNativeToolPassthrough?: boolean // Codex 原生工具透传（默认 true）
  codexToolCompat?: boolean // Codex 工具兼容（默认 true）
  normalizeNonstandardChatRoles?: boolean // OpenAI Chat 上游：将非标准 role 改写为 user（默认 true）
  stripCodexClientTools?: boolean // Responses 上游：透传前剥离 Codex CLI 0.130+ 客户端专属工具条目（默认 true）
  stripImageGenerationTool?: boolean // Responses/Chat 上游：移除 image_generation 工具（默认 true）
  convertImageUrlToB64Json?: boolean // Images 上游：将仅返回 URL 的 b64_json 请求响应转换为 base64
  latency?: number
  status?: ChannelDisplayStatus
  index: number
  pinned?: boolean
  // 多渠道调度相关字段
  priority?: number // 渠道优先级（数字越小优先级越高）
  metrics?: ChannelMetrics // 实时指标
  suspendReason?: string // 熔断原因
  promotionUntil?: string // 促销期截止时间（ISO 格式）
  latencyTestTime?: number // 延迟测试时间戳（用于 5 分钟后自动清除显示）
  lowQuality?: boolean // 低质量渠道标记：启用后强制本地估算 token，偏差>5%时使用本地值
  racing?: { enabled?: boolean } // 渠道级竞速参与开关（不参与=不做主触发也不做影子目标）
  balanceCheck?: ChannelBalanceCheckConfig // 渠道级上游余额探测配置（nil=继承全局与托管默认）
  injectDummyThoughtSignature?: boolean // Gemini 特定：为 functionCall 注入 dummy thought_signature（兼容第三方 API）
  stripThoughtSignature?: boolean // Gemini 特定：移除 thought_signature 字段（兼容旧版 Gemini API）
  passbackReasoningContent?: boolean // Claude 协议特定：将 thinking 块转为 reasoning_content 回传（兼容 mimo 等上游）
  passbackThinkingBlocks?: boolean // Claude 协议特定：将真实 reasoning_content 投影为 content[].thinking（兼容 DeepSeek/GLM 等严格 thinking 上游）
  supportedModels?: string[] // 支持的模型白名单（空=全部），支持通配符如 gpt-4*
  noVision?: boolean // 整个渠道不支持图片输入
  // 主动限速（渠道级生产代理限速，区别于能力测试的 rpm）
  rateLimitRpm?: number // 每分钟请求数上限（0/空=不限）
  rateLimitWindowMinutes?: number // 滑动窗口时长（秒，0/空=默认60秒）
  rateLimitBurst?: number // 已废弃，保留仅为兼容性
  rateLimitMaxConcurrent?: number // 最大并发上游请求数（0/空=不限）
  rateLimitAutoFromHeaders?: boolean // 自动从上游响应头解析限流信息并动态调速（默认 true）
  historicalImageTurnLimit?: number // 历史图片轮次限制（0=不限制，2-10=裁剪历史图片）
  compactModel?: string // 本地 compact 时使用的上游模型名（为空则使用原始请求的模型）
  autoManaged?: boolean // 启用自动托管
  autoManagedAt?: string // 开始托管时间（ISO 格式）
  autoManagedKind?: string // 托管子类型："" | "generic" | "new_api"
  originType?: string // 渠道来源类型
  originTier?: string // 渠道来源可信层级
  learnedClientFingerprint?: boolean // 已学习：上游 models 端点存在客户端指纹校验，探测/保活请求自动带 Claude Code 伪装头
  subscriptionUid?: string // 关联的订阅 UID（new-api 自动托管场景）
  rpm?: number // 能力测试发送速率（仅影响能力测试）
  tags?: string[] // 用户自定义标签（自由文本，与 PoolTag 完全独立）
}

export interface ChannelProtocolCapsule {
  kind: ChannelKind
  label: string
  serviceType: string
  channelUid?: string
  index: number
  status?: ChannelDisplayStatus
}

export interface ChannelModelBinding {
  credentialUid?: string
  keyMask: string
  models: string[]
  updatedAt?: string
  modelsDiscoveredAt?: string
  modelDiscoverySource?: string
  modelDiscoveryMessage?: string
}

export interface ChannelProtocolRoute {
  claudeDesktopModelTiers?: Record<string, string>
  claudeDesktopTierRules?: Readonly<Record<string, string>>
  kind: ChannelKind
  upstreamKind?: ChannelKind
  index: number
  name: string
  responsesReasoningFold?: boolean
  serviceType: string
  channelUid?: string
  status?: ChannelDisplayStatus
  apiKeys?: string[]
  apiKeyConfigs?: APIKeyConfig[]
  disabledApiKeys?: DisabledKeyInfo[]
  disabledKeyModels?: DisabledKeyModelInfo[]
  disabledGroupModels?: DisabledGroupModelInfo[]
  supportedModels?: string[]
  modelInventoryKnown?: boolean
  discoveredModels?: string[]
  modelBindings?: ChannelModelBinding[]
  modelsUpdatedAt?: string
  modelsDiscoveredAt?: string
  modelDiscoverySource?: string
  modelDiscoveryMessage?: string
  configured?: boolean
}

export interface ChannelsResponse {
  channels: Channel[]
  current: number
}

// 渠道仪表盘响应（合并 channels + metrics + stats）
export interface ChannelDashboardResponse {
  channels: Channel[]
  metrics: ChannelMetrics[]
  stats: SchedulerStatsResponse
  recentActivity?: ChannelRecentActivity[] // 最近 15 分钟分段活跃度
}

export interface LlmChannelDashboardResponse {
  dashboards: Record<'messages' | 'chat' | 'responses' | 'gemini', ChannelDashboardResponse>
}

export interface PingResult {
  success: boolean
  latency: number
  status: string
  error?: string
}

export interface ResumeChannelResponse {
  success: boolean
  message: string
  restoredKeys?: number
}

// ============== 逻辑渠道（Logical Channel）类型 ==============

export interface LogicalChannelProtocol {
  kind: string // messages / chat / responses / gemini / images / vectors
  channelUid: string
  serviceType: string // claude / openai / responses / gemini
  enabled: boolean
  status: string
  priority: number
  routePrefix: string
}

export type LogicalChannelKind = 'llm' | 'embeddings' | 'images'

export interface LogicalChannel {
  logicalChannelUid: string
  accountUid?: string
  providerId?: string
  name: string
  remark?: string
  description?: string
  website?: string
  kind: LogicalChannelKind
  baseUrls: string[]
  siteIdentity: string
  protocols: LogicalChannelProtocol[]
  tags?: string[]
  healthTag?: string
  qualityTag?: string
  costTag?: string
  capabilityTags?: string[]
  createdAt: string
  updatedAt: string
}

export interface LogicalChannelsResponse {
  logicalChannels: LogicalChannel[]
}

export interface CreateLogicalChannelProtocol {
  kind: string
  serviceType: string
  apiKeys?: string[]
  apiKeyConfigs?: APIKeyConfig[]
  baseUrls?: string[]
  baseUrl?: string
  priority?: number
  enabled?: boolean
  status?: string
  routePrefix?: string
  supportedModels?: string[]
  customHeaders?: Record<string, string>
  proxyUrl?: string
  outboundNetwork?: OutboundNetworkConfig | null
  proxyPreferDirect?: boolean
}

export interface CreateLogicalChannelRequest {
  name: string
  remark?: string
  description?: string
  website?: string
  providerId?: string
  accountUid?: string
  kind?: LogicalChannelKind
  baseUrls?: string[]
  apiKeys?: string[]
  tags?: string[]
  protocols: CreateLogicalChannelProtocol[]
  placement?: string
}

export interface DeleteLogicalChannelResponse {
  message: string
  logicalChannelUid: string
  removedChannels: string[]
}
