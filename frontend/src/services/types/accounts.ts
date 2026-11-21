import type { DiscoveryRateLimitResult } from './capabilities'
import type { ChannelModelBinding } from './channels'
import type { ChannelKind } from './common'

// 自动添加渠道请求
export interface AutoAddChannelRequest {
  name?: string
  // provider 模板模式：带 providerId + apiKeys，baseURL 由后端按 key 前缀探测判定，baseUrls 可省略
  providerId?: string
  baseUrls?: string[]
  apiKeys: string[]
  rateLimitHint?: DiscoveryRateLimitResult
  subscriptionUid?: string
}

// 自动添加渠道响应
export interface AutoAddChannelResponse {
  accountUid: string
  channelUid: string
  index: number
  discoveryStarted: boolean
  channels?: AutoAddChannelResult[]
}

export interface AutoAddChannelResult {
  accountUid: string
  channelKind: string
  channelUid: string
  index: number
  name: string
  serviceType: string
  discoveryStarted: boolean
}

export interface UpdateManagedAccountResponse {
  accountUid: string
  keyCount: number
  channelCount: number
  discoveryStarted: number
  /** 降级放行的验证警告：非鉴权类探测失败，key 已保存但连通性未确认。 */
  warnings?: string[]
}

export interface ManagedAccountCredential {
  credentialUid: string
  keyMask: string
  hasVolcengineAccessKey?: boolean
  volcengineAccessKeyIdMask?: string
  volcenginePlan?: 'agent_plan' | 'coding_plan'
  volcenginePlanTier?: string
  volcenginePlanStatus?: string
  volcenginePlanUsage?: VolcenginePlanUsage
  volcenginePlanBuckets?: VolcenginePlanBucket[]
  hasMiMoConsoleCookie?: boolean
  mimoTokenPlan?: MiMoTokenPlanSnapshot
  hasCompshareConsoleCookie?: boolean
  compsharePlan?: CompsharePlanSnapshot
  hasKimiConsoleToken?: boolean
  kimiCodeUsage?: KimiCodeUsageSnapshot
}

export interface DeepSeekBalanceInfo {
  currency: 'CNY' | 'USD' | string
  totalBalance: string
  grantedBalance: string
  toppedUpBalance: string
}

export interface DeepSeekCredentialBalance {
  credentialUid: string
  keyMask: string
  isAvailable: boolean
  balanceInfos?: DeepSeekBalanceInfo[]
  fetchedAt: string
  error?: string
}

export interface DeepSeekAccountBalancesResponse {
  accountUid: string
  balances: DeepSeekCredentialBalance[]
}

/** 火山套餐单个时间窗口用量。Agent Plan 含 quota/used，Coding Plan 含 usedPercent。 */
export interface VolcenginePlanUsageWindow {
  quota?: number
  used: number
  usedPercent?: number
  resetTime?: number
}

/**
 * 火山套餐用量快照。
 * Agent Plan 填充 fiveHour/daily/weekly/monthly（含 quota）；
 * Coding Plan 填充 fiveHour/weekly/monthly（仅 usedPercent）。
 */
export interface VolcenginePlanUsage {
  fiveHour?: VolcenginePlanUsageWindow
  daily?: VolcenginePlanUsageWindow
  weekly?: VolcenginePlanUsageWindow
  monthly?: VolcenginePlanUsageWindow
  fetchedAt: string
  error?: string
}

export interface VolcenginePlanUsageRefreshResponse {
  usage: VolcenginePlanUsage
  cached: boolean
  plans?: VolcenginePlanBucket[]
}

/** 火山套餐单桶快照：{agent|coding}_plan × {personal|team}，团队版带席位绑定。 */
export interface VolcenginePlanBucket {
  product: 'agent_plan' | 'coding_plan'
  edition: 'personal' | 'team'
  seatId?: string
  tier?: string
  status?: string
  usage?: VolcenginePlanUsage
  error?: string
}

export interface MiMoTokenPlanQuota {
  used: number
  limit: number
  usedPercent: number
}

export interface MiMoTokenPlanSnapshot {
  planCode: string
  planName: string
  currentPeriodEnd: string
  expired: boolean
  monthUsage: MiMoTokenPlanQuota
  currentUsage: MiMoTokenPlanQuota
  validatedAt: string
}

export interface MiMoConsoleCookieResponse {
  accountUid: string
  credentialUid: string
  keyAdopted: boolean
  keyMask: string
  adoptedApiKey?: string
  tokenPlan: MiMoTokenPlanSnapshot
  discoveryStarted: number
}

export interface CompsharePlanUsageWindow {
  used: number
  limit: number
  updatedAt?: number
  nextResetAt?: number
}

export interface CompsharePlanSnapshot {
  planCode: string
  planName: string
  displayName: string
  status: number
  concurrencyLimit: number
  isTeam: boolean
  expireAt: number
  fiveHourUsage: CompsharePlanUsageWindow
  weeklyUsage: CompsharePlanUsageWindow
  monthlyUsage: CompsharePlanUsageWindow
  validatedAt: string
}

export interface CompshareConsoleCookieResponse {
  accountUid: string
  credentialUid: string
  plan: CompsharePlanSnapshot
}

export interface KimiCodeQuotaWindow {
  used: number
  limit: number
  remaining: number
  resetTime?: string
}

export interface KimiCodeRateLimit {
  windowSeconds: number
  usage: KimiCodeQuotaWindow
}

export interface KimiCodeRatioWindow {
  ratio: number
  enabled: boolean
  resetTime?: string
}

export interface KimiCodeMoney {
  currency?: string
  priceInCents: number
}

export interface KimiBoosterWallet {
  id?: string
  status?: string
  allowTopup: boolean
  moneyLeft: KimiCodeMoney
  moneyTotal: KimiCodeMoney
  monthlyChargeLimit: KimiCodeMoney
  monthlyUsed: KimiCodeMoney
}

export interface KimiCodeBalance {
  feature?: string
  type?: string
  unit?: string
  amountUsedRatio: number
  kimiCodeUsedRatio: number
  expireTime?: string
}

export interface KimiCodeUsageSnapshot {
  weeklyUsage: KimiCodeQuotaWindow
  totalQuota: KimiCodeQuotaWindow
  rateLimits?: KimiCodeRateLimit[]
  codeFiveHour?: KimiCodeRatioWindow
  codeSevenDay?: KimiCodeRatioWindow
  subscriptionBalance?: KimiCodeBalance
  giftBalances?: KimiCodeBalance[]
  boosterWallets?: KimiBoosterWallet[]
  validatedAt: string
}

export interface KimiConsoleTokenResponse {
  accountUid: string
  credentialUid: string
  usage: KimiCodeUsageSnapshot
}

export interface VolcengineAccessKeyResponse {
  accountUid: string
  credentialUid: string
  accessKeyIdMask: string
  plan: 'agent_plan' | 'coding_plan'
  planTier: string
  planStatus: string
  usage?: VolcenginePlanUsage
  discoveryStarted: number
}

export interface ManagedAccountChannel {
  kind: ChannelKind
  channelUid: string
  name: string
  serviceType: string
  status: string
  modelInventoryKnown?: boolean
  discoveredModels?: string[]
  modelBindings?: ChannelModelBinding[]
  modelsUpdatedAt?: string
  modelsDiscoveredAt?: string
  modelDiscoverySource?: string
  modelDiscoveryMessage?: string
  protocolAvailability?: ProtocolAvailability[]
}

export interface ProtocolAvailability {
  protocol: ChannelKind
  modelInventoryKnown?: boolean
  discoveredModels?: string[]
  modelBindings?: ChannelModelBinding[]
  modelsDiscoveredAt?: string
  modelDiscoverySource?: string
  modelDiscoveryMessage?: string
}

export interface ManagedAccount {
  accountUid: string
  providerId: string
  name: string
  credentials: ManagedAccountCredential[]
  channels: ManagedAccountChannel[]
  endpointCount: number
}

export interface ManagedAccountsResponse {
  accounts: ManagedAccount[]
}

// Endpoint 发现信息
export interface EndpointDiscoveryInfo {
  keyMask: string
  baseUrl: string
  modelsCount: number
  protocolOk: boolean
  modelDiscoverySource?: string
  modelDiscoveryMessage?: string
  modelsDiscoveredAt?: string
  protocolModels?: Partial<Record<ChannelKind, string[]>>
  protocolDiscoveredAt?: Partial<Record<ChannelKind, string>>
  protocolDiscoverySource?: Partial<Record<ChannelKind, string>>
  protocolDiscoveryMessage?: Partial<Record<ChannelKind, string>>
  protocolDiscoveryError?: Partial<Record<ChannelKind, string>>
}

// 发现状态信息
export interface DiscoveryStatusInfo {
  status: 'idle' | 'pending' | 'running' | 'done' | 'failed'
  startedAt?: string
  finishedAt?: string
  error?: string
  endpoints?: EndpointDiscoveryInfo[]
}

// 自动托管状态响应
export interface ChannelAutoStatusResponse {
  autoManaged: boolean
  autoManagedAt?: string
  discovery?: DiscoveryStatusInfo
}
