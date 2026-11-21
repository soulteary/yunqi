export interface ChannelSequenceEntry {
  channelIndex: number
  channelName: string
}

export interface ConversationInfo {
  id: string
  kind: 'messages' | 'responses' | 'chat' | 'gemini' | 'images' | 'vectors'
  userId: string
  rawUserId?: string
  title?: string
  createdAt: string
  lastActiveAt: string
  requestCount: number
  models: string[]
  currentChannel: number
  channelName: string
  status: 'active' | 'streaming' | 'idle'
  lastModel: string
  lastRequestId: string
  lastUserMessage?: string
  lastUserMessages?: string[]
  lastRecap?: string
  lastRecapAt?: string
  parentThreadId?: string
  parentConversationId?: string
  childConversationIds?: string[]

  // subagent 观测（仅展示，不影响路由）
  hasSubagents?: boolean
  subagentCount?: number
  mainChannel?: number
  subagentChannel?: number
}

export interface SequenceOverrideInfo {
  sequence: ChannelSequenceEntry[]
  hasMainSequence?: boolean
  subagentSequence?: ChannelSequenceEntry[] // subagent 专用序列（为空时 fallback 到 sequence）
  setAt: string
  expiresAt: string
  isPerpetual?: boolean
}

export interface ConversationsResponse {
  conversations: ConversationInfo[]
  total: number
  overrides: Record<string, SequenceOverrideInfo>
  channelsByKind?: Record<
    string,
    { index: number; name: string; priority: number; status: string; circuitOpen?: boolean }[]
  >
}
