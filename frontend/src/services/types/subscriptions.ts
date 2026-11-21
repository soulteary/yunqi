export interface BillingTermsPatch {
  paymentAmount: number | null
  paymentUnit: string
  creditAmount: number | null
  creditUnit: string
  expectedVersion?: number
}

export interface BillingTermsResponse {
  paymentAmount?: number
  paymentUnit?: string
  creditAmount?: number
  creditUnit?: string
  version: number
  preview: string
}

export interface NewApiKeyStatus {
  keyUid?: string
  name: string
  group: string
  groupMultiplier: number
  maxGroupMultiplier: number
  sourceRemoteTokenId: number
  syncStatus: string
  reason?: string
  multiplierExpiresAt?: string
  updatedAt?: string
}

export interface NewApiSyncResult {
  subscriptionUid: string
  success: boolean
  balance?: number
  usedQuota?: number
  models?: string[]
  modelsHash?: string
  modelsHashChanged: boolean
  keys: NewApiKeyStatus[]
  discoveryTriggered: boolean
  failedReason?: string
}

export interface BalanceRefreshResult {
  success: boolean
  balance: number
  currency: string
  errorMessage: string
}

export interface SubscriptionRefreshResponse {
  subscription: SubscriptionItem
  refreshResult: NewApiSyncResult | BalanceRefreshResult
}

// ============== 订阅中心类型 ==============

export interface SubscriptionItem {
  subscriptionUid: string
  displayName: string
  provider?: string
  originType?: string
  originTier?: string
  billingMode?: string
  currency?: string
  balance?: number
  usedQuota?: number
  groupMultipliers?: Record<string, number>
  paymentAmount?: number | null
  paymentUnit?: string
  creditAmount?: number | null
  creditUnit?: string
  version: number
  authTokenMode?: 'bearer' | 'raw' | 'raw_auth' | string
  baseUrl?: string
  accessTokenMasked?: string
  userId?: string
  username?: string
  linkedChannelUids?: string[]
  source?: string
  confidence?: number
  notes?: string
  createdAt: string
  updatedAt: string
  archivedAt?: string

  // Phase 4 Item 6: 余额自动刷新
  billingApiKey?: string
  autoRefreshEnabled?: boolean
  autoRefreshSupported?: boolean
  lastBalanceRefreshAt?: string
  lastBalanceRefreshError?: string

  // NewAPI 自动接入的分组安全阈值与已绑定分组快照
  provisionGroup?: string
  provisionGroupRatio?: number
  maxGroupMultiplier?: number
  provisionedKeys?: NewApiProvisionedKeyInfo[]

  // 站点访问代理（地域封锁场景），绑定/同步经此代理
  proxyUrl?: string
  proxyPreferDirect?: boolean
}

export interface SubscriptionsListResponse {
  subscriptions: SubscriptionItem[]
  total: number
}

export interface SubscriptionCreateRequest {
  subscriptionUid: string
  displayName: string
  provider?: string
  originType?: string
  originTier?: string
  billingMode?: string
  currency?: string
  balance?: number
  groupMultipliers?: Record<string, number>
  notes?: string
  source?: string

  // Phase 4 Item 6: 余额自动刷新
  billingApiKey?: string
  autoRefreshEnabled?: boolean
}

export interface SubscriptionUpdateRequest {
  displayName?: string
  provider?: string
  originType?: string
  originTier?: string
  billingMode?: string
  currency?: string
  balance?: number
  groupMultipliers?: Record<string, number>
  notes?: string
  source?: string
  confidence?: number

  // Phase 4 Item 6: 余额自动刷新
  billingApiKey?: string
  autoRefreshEnabled?: boolean
  accessToken?: string
  userId?: string
  authTokenMode?: string
  expectedVersion?: number
}

// ============== new-api 订阅集成类型（§8.5.1） ==============

export interface NewApiVerifyRequest {
  baseUrl: string
  accessToken: string
  userId?: string
  authTokenMode?: string
  displayName?: string
  subscriptionUid?: string
  /** 站点访问代理（地域封锁场景） */
  proxyUrl?: string
  proxyPreferDirect?: boolean
}

export interface NewApiVerifyResponse {
  username: string
  userId: number
  quota: number
  usedQuota: number
  groups: Record<string, number>
  groupFetchError?: string
  /** 各分组可用模型数（GET /api/user/models?group=）；某组缺记录表示查询失败或 fork 不支持 */
  groupModelCounts?: Record<string, number>
  availableModels: string[]
  suggestedOriginType: string
  suggestedOriginTier: string
  accessTokenMasked: string
}

export interface NewApiProvisionRequest {
  subscriptionUid: string
  displayName: string
  baseUrl: string
  accessToken: string
  channelKind: string
  userId?: string
  authTokenMode?: string
  channelName?: string
  provisionKeyName?: string
  provisionGroup?: string
  provisionAllEligibleGroups?: boolean
  provisionModels?: string[]
  maxGroupMultiplier?: number
  notes?: string
  /** 站点访问代理（地域封锁场景），绑定成功后写入渠道 */
  proxyUrl?: string
  proxyPreferDirect?: boolean
}

export interface NewApiProvisionedKeyInfo {
  name: string
  group: string
  groupMultiplier: number
  tokenId: number
}

export interface NewApiProvisionedKey extends NewApiProvisionedKeyInfo {
  reused: boolean
}

export interface NewApiProvisionResponse {
  subscription: SubscriptionItem
  channelUid: string
  channelIndex: number
  channelName?: string
  /** true 表示同站点已有渠道，key 已并入而非新建 */
  mergedChannel?: boolean
  provisionedKey: string
  provisionedTokenId: number
  reused: boolean
  provisionedKeys?: NewApiProvisionedKey[]
  /** 因可用模型数为 0 被跳过建 key 的分组 */
  skippedEmptyGroups?: string[]
  discoveryStarted: boolean
}

// ============== new-api 多账号类型 ==============

export interface NewApiAccountCreateRequest {
  accessToken: string
  userId?: string
  displayName?: string
  authTokenMode?: string
  provisionModels?: string[]
  maxGroupMultiplier?: number
  provisionAllEligibleGroups?: boolean
  /** 账号级代理覆盖，空=继承订阅级代理设置 */
  proxyUrl?: string
  proxyPreferDirect?: boolean
}

export interface NewApiCredentialsUpdateRequest {
  accessToken?: string
  userId?: string
  authTokenMode?: string
  proxyUrl?: string
  proxyPreferDirect?: boolean
  expectedVersion?: number
}

/** 更新 new-api 子账号凭证；代理不在账号级维护（渠道"代理通道"是唯一事实源） */
export interface NewApiAccountCredentialsUpdateRequest {
  accessToken?: string
  userId?: string
  authTokenMode?: string
}

export interface NewApiAccountItem {
  accountUid: string
  userId?: string
  /** 令牌注入 Authorization 头的方式（bearer/raw），非敏感 */
  authTokenMode?: string
  displayName?: string
  balance?: number
  status?: string
  accessTokenMasked?: string
  /** 该账号在远端自动接入的分组 key（明文不落库，仅元数据） */
  provisionedKeys?: NewApiProvisionedKeyInfo[]
  /** 最近一次分组/倍率同步失败原因 */
  lastSyncError?: string
  lastCheckedAt?: string
  usedQuota?: number
  createdAt: string
  /** 账号级代理设置（空表示继承订阅级） */
  proxyUrl?: string
  proxyPreferDirect?: boolean
}

export interface NewApiAccountListResponse {
  accounts: NewApiAccountItem[]
}
