import type { ApiClient } from './client'
import type {
  ChannelBalanceView,
  HealthCenterChannelsResponse,
  HealthCenterEndpointsResponse,
  HealthCenterOverview,
  TokenPlanUsageRefreshResponse
} from '../types/health'

export function createHealthApi(client: ApiClient) {
  return {
    /** 查询渠道上游余额探测状态（策略 + key 明细 + 配额真相维度值） */
    async getChannelBalance(channelType: string, channelId: number): Promise<ChannelBalanceView> {
      return client.request(`/${channelType}/channels/${channelId}/balance`)
    },

    /** 手动触发渠道余额探测（202 异步，结果通过 getChannelBalance 查询） */
    async triggerChannelBalanceCheck(channelType: string, channelId: number): Promise<{ queued: boolean }> {
      return client.request(`/${channelType}/channels/${channelId}/balance/check`, { method: 'POST' })
    },

    async getHealthCenterOverview(): Promise<HealthCenterOverview> {
      return client.request('/health-center/overview')
    },

    async getHealthCenterChannels(): Promise<HealthCenterChannelsResponse> {
      return client.request('/health-center/channels')
    },

    async getHealthCenterChannelEndpoints(channelUid: string): Promise<HealthCenterEndpointsResponse> {
      return client.request(`/health-center/channels/${encodeURIComponent(channelUid)}/endpoints`)
    },

    async refreshEndpointTokenPlanUsage(endpointUid: string): Promise<TokenPlanUsageRefreshResponse> {
      return client.request(`/health-center/endpoints/${encodeURIComponent(endpointUid)}/token-plan-usage/refresh`, {
        method: 'POST'
      })
    }
  }
}

export type HealthApi = ReturnType<typeof createHealthApi>
