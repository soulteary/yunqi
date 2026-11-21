// ============== 画像变更事件（Phase 3A） ==============

export type ProfileChangeEventType =
  | 'profile_updated'
  | 'health_changed'
  | 'discovery_completed'
  | 'auto_mapping_applied'

export interface ProfileChangeEvent {
  eventUid: string
  channelUid: string
  channelKind: string
  endpointUid?: string
  metricsKey?: string
  eventType: ProfileChangeEventType
  summary: string
  oldValue?: string
  newValue?: string
  createdAt: string
}

export interface ProfileChangelogResponse {
  events: ProfileChangeEvent[]
  total: number
}

// ============== 跨模块状态事件（Phase B） ==============

/** 状态事件类型（对应 backend eventbus.Type* 常量） */
export type StateEventType =
  | 'circuit_breaker_state_changed'
  | 'key_blacklisted'
  | 'key_restored'
  | 'key_model_disabled'
  | 'key_model_restored'
  | 'config_reloaded'
  | 'upstream_changed'
  | 'channel_status_changed'
  | 'logical_channel_rebuilt'
  | 'preset_bundle_swapped'
  | 'manifest_drift'
  | 'capability_drift'

/** 状态事件作用域（发布来源模块） */
export type StateEventScope = 'metrics' | 'config' | 'preset'

/** 跨模块状态事件 envelope（对应 backend eventbus.Event）。仅承载脱敏字段。 */
export interface StateEvent {
  uid: string
  type: StateEventType
  scope: StateEventScope
  /** channelUID / metricsKey / logicalChannelUid 等 */
  subject?: string
  /** messages / chat / ...（可选） */
  channelKind?: string
  /** 状态迁移前值 */
  from?: string
  /** 状态迁移后值 */
  to?: string
  cause?: string
  payload?: Record<string, unknown>
  createdAt: string
}

export interface StateEventsResponse {
  events: StateEvent[]
  total: number
}

/** manifest_drift 事件 payload */
export interface ManifestDriftPayload {
  /** 新增模型 */
  added?: string[]
  /** 移除模型 */
  removed?: string[]
  /** 发现来源端点（回填上下文，可选） */
  baseURL?: string
  /** 清单来源（如 control_plane，可选） */
  source?: string
  /** 发现时间 RFC3339（可选） */
  discoveredAt?: string
}

/** capability_drift 事件 payload */
export interface CapabilityDriftPayload {
  /** 探测模型 */
  model?: string
  /** 经 model mapping 重定向后实际发往上游的模型 */
  actualModel?: string
  /** 探测协议 */
  protocol?: string
  /** 渠道可读名 */
  channelName?: string
  /** 注册表声明的能力 */
  declared?: {
    source?: string
    matchedPattern?: string
    contextWindow?: number
    maxOutput?: number
    thinkingMode?: string
    reasoningEfforts?: string[]
  }
  /** 实际探测结果 */
  actual?: {
    success?: boolean
    streamingSupported?: boolean
    latencyMs?: number
    error?: string
  }
  /** 漂移字段列表 */
  driftFields?: string[]
}
