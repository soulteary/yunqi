import type { ApiClient } from './client'
import type {
  ABTestEmergencyStopResponse,
  ABTestResultsResponse,
  CockpitOverviewResponse,
  CreateIntentRequest,
  IntentListResponse,
  ManualRoutingIntent,
  RecommendationsResponse,
  SmartRoutingConfig
} from '../types/routing'
import type {
  AutopilotTraceDetailResponse,
  AutopilotTraceListResponse,
  AutopilotTraceStats
} from '../types/routing-traces'

export function createRoutingApi(client: ApiClient) {
  return {
    async getCockpitOverview(): Promise<CockpitOverviewResponse> {
      return client.request('/cockpit/overview')
    },

    /** 查询试用意图列表。all=true 返回全部（含 expired/exhausted/disabled），默认只返回 active */
    async getManualIntents(params?: { all?: boolean }): Promise<IntentListResponse> {
      const query = new URLSearchParams()
      if (params?.all) query.set('all', 'true')
      const qs = query.toString()
      return client.request(`/manual-intents${qs ? `?${qs}` : ''}`)
    },

    /** 查询单条试用意图详情 */
    async getManualIntent(uid: string): Promise<ManualRoutingIntent> {
      return client.request(`/manual-intents/${encodeURIComponent(uid)}`)
    },

    /** 创建试用意图 */
    async createManualIntent(data: CreateIntentRequest): Promise<ManualRoutingIntent> {
      return client.request('/manual-intents', {
        method: 'POST',
        body: JSON.stringify(data)
      })
    },

    /** 删除（结束）试用意图 */
    async deleteManualIntent(uid: string): Promise<void> {
      await client.request(`/manual-intents/${encodeURIComponent(uid)}`, {
        method: 'DELETE'
      })
    },

    /** 获取渠道推荐列表。proxyKeyMask 缺省时聚合返回全部用户的推荐。 */
    async getRecommendations(params?: {
      proxyKeyMask?: string
      windowDays?: number
    }): Promise<RecommendationsResponse> {
      const query = new URLSearchParams()
      if (params?.proxyKeyMask) query.set('proxyKeyMask', params.proxyKeyMask)
      if (params?.windowDays) query.set('windowDays', String(params.windowDays))
      const qs = query.toString()
      return client.request(`/autopilot/recommendations${qs ? `?${qs}` : ''}`)
    },

    /** 获取智能路由全局配置 */
    async getSmartRoutingConfig(): Promise<SmartRoutingConfig> {
      return client.request('/smart-routing/config')
    },

    /** 更新智能路由全局配置 */
    async updateSmartRoutingConfig(data: Partial<SmartRoutingConfig>): Promise<SmartRoutingConfig> {
      return client.request('/smart-routing/config', {
        method: 'PUT',
        body: JSON.stringify(data)
      })
    },

    /** 获取路由决策追踪列表 */
    async getAutopilotTraces(params?: {
      limit?: number
      mismatch?: boolean
      release?: string
      cohort?: string
      mode?: string
    }): Promise<AutopilotTraceListResponse> {
      const query = new URLSearchParams()
      if (params?.limit) query.set('limit', String(params.limit))
      if (params?.mismatch) query.set('mismatch', 'true')
      if (params?.release) query.set('release', params.release)
      if (params?.cohort) query.set('cohort', params.cohort)
      if (params?.mode) query.set('mode', params.mode)
      const qs = query.toString()
      return client.request(`/traces${qs ? '?' + qs : ''}`)
    },

    /** 获取单条路由追踪详情 */
    async getAutopilotTraceDetail(traceUid: string): Promise<AutopilotTraceDetailResponse> {
      return client.request(`/traces/${encodeURIComponent(traceUid)}`)
    },

    /** 获取路由追踪统计汇总 */
    async getAutopilotTraceStats(): Promise<AutopilotTraceStats> {
      return client.request('/traces/stats')
    },

    /** 获取 A/B 测试结果统计 */
    async getABTestResults(): Promise<ABTestResultsResponse> {
      return client.request('/autopilot/ab-test-results')
    },

    /** 紧急停止 A/B 测试 */
    async emergencyStopABTest(reason?: string): Promise<ABTestEmergencyStopResponse> {
      return client.request('/autopilot/ab-test/emergency-stop', {
        method: 'POST',
        body: JSON.stringify({ reason })
      })
    }
  }
}

export type RoutingApi = ReturnType<typeof createRoutingApi>
