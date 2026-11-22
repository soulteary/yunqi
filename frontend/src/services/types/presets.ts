import type { ClaudeMessagesPreset } from '../../generated/claudeMessagesPresets'
import type { CodexResponsesPreset } from '../../generated/codexResponsesPresets'
import type { OpenAIChatPreset } from '../../generated/openaiChatPresets'
import type { OpenAIMessagesPreset } from '../../generated/openaiMessagesPresets'
import type { ModelBenchmarkProfile, UpstreamModelCapability } from './models'

// Provider 模板 key 前缀规则
export interface ProviderKeyPrefixRule {
  prefix: string
  planTag: string
}

// Provider 候选 baseURL
export interface ProviderCandidate {
  baseUrl: string
  planTag?: string
  region?: string
  priority?: number
}

// Provider 在某个 Yunqi 协议渠道下的原生上游入口
export interface ProviderRoute {
  channelKind: string
  serviceType: string
  description?: string
  candidates?: ProviderCandidate[]
}

// 已知 provider 模板（模板化添加：选 provider + 输 key，系统自动判别 plan/baseURL）
export interface ProviderTemplate {
  providerId: string
  aliases?: string[]
  displayName: string
  description?: string
  channelKind: string
  serviceType: string
  originType?: string
  originTier?: string
  keyPrefixRules?: ProviderKeyPrefixRule[]
  candidates?: ProviderCandidate[]
  routes?: ProviderRoute[]
}

// GET /channels/provider-templates 响应
export interface ProviderTemplatesResponse {
  providers: ProviderTemplate[]
}

// ─── 预置数据（GET /api/presets）───────────────────────────────────────────
// 与后端 presetstore.PresetBundle 对齐；前端订阅表单选项由此派生，取代硬编码副本。

export interface PresetOriginTypeEntry {
  value: string
  tier: string
}

export interface PresetNewApiDefaults {
  originType: string
  originTier: string
  billingMode: string
}

export interface SubscriptionPreset {
  originTypes: PresetOriginTypeEntry[]
  billingModes: string[]
  sources: string[]
  autoRefreshProviders: string[]
  newApiDefaults: PresetNewApiDefaults
  originTypeAliases: Record<string, string>
}

export interface WrappedPresetCollection<T> {
  schemaVersion?: number
  providers?: Record<string, T>
  presets?: Record<string, T>
}

export interface RuntimeModelRegistryEntry extends UpstreamModelCapability {
  patterns?: string[]
}

export interface RuntimeModelBenchmarkProfile extends ModelBenchmarkProfile {
  patterns?: string[]
}

export interface RuntimeModelRegistryBundle {
  schemaVersion?: number
  pricingUnit?: string
  upstreamCapabilities?: RuntimeModelRegistryEntry[]
  benchmarkProfiles?: RuntimeModelBenchmarkProfile[]
}

export interface ChannelPresetBundle {
  schemaVersion?: number
  claudeMessages?: Record<string, ClaudeMessagesPreset> | WrappedPresetCollection<ClaudeMessagesPreset>
  openAIChat?: Record<string, OpenAIChatPreset> | WrappedPresetCollection<OpenAIChatPreset>
  codexResponses?: Record<string, CodexResponsesPreset> | WrappedPresetCollection<CodexResponsesPreset>
  openAIMessages?: Record<string, OpenAIMessagesPreset> | WrappedPresetCollection<OpenAIMessagesPreset>
}

export interface PresetBundle {
  schemaVersion: number
  dataVersion: string
  subscription: SubscriptionPreset
  modelRegistry?: Record<string, UpstreamModelCapability> | RuntimeModelRegistryBundle
  channelPresets?: ChannelPresetBundle
  builtinModelsManifests?: Record<string, unknown>
}
