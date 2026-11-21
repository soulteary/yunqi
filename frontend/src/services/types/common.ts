// API 数据结构类型
export type ChannelStatus = 'active' | 'suspended' | 'disabled'

export type ChannelDisplayStatus = ChannelStatus | 'partial' | 'healthy' | 'error' | 'unknown' | ''

// 新增渠道在故障转移序列中的放置位置：front（首位）| back（末尾，默认）
export type ChannelPlacement = 'front' | 'back'

export type CircuitState = 'closed' | 'open' | 'half_open'

export type ChannelAuthHeader = 'auto' | 'bearer' | 'x-api-key'

export type ChannelKind = 'messages' | 'chat' | 'responses' | 'gemini' | 'images' | 'vectors'
