import type { ApiClient } from '../client'
import type { Channel, ChannelsResponse, PingResult, ResumeChannelResponse } from '../../types/channels'
import type { ChannelPlacement, ChannelStatus } from '../../types/common'
import type { ChannelModelsRequest, ModelsResponse } from '../../types/models'

export function createChannelsResponsesApi(client: ApiClient) {
  return {
    async getResponsesChannels(): Promise<ChannelsResponse> {
      return client.request('/responses/channels')
    },

    async addResponsesChannel(
      channel: Omit<Channel, 'index' | 'latency' | 'status'>,
      placement?: ChannelPlacement
    ): Promise<void> {
      await client.request('/responses/channels', {
        method: 'POST',
        body: JSON.stringify(placement ? { ...channel, placement } : channel)
      })
    },

    async pingResponsesChannel(id: number): Promise<PingResult> {
      return client.request(`/responses/ping/${id}`)
    },

    async pingAllResponsesChannels(): Promise<Array<{ id: number; name: string; latency: number; status: string }>> {
      return client.request('/responses/ping')
    },

    async updateResponsesChannel(id: number, channel: Partial<Channel>): Promise<void> {
      await client.request(`/responses/channels/${id}`, {
        method: 'PUT',
        body: JSON.stringify(channel)
      })
    },

    async deleteResponsesChannel(id: number): Promise<void> {
      await client.request(`/responses/channels/${id}`, {
        method: 'DELETE'
      })
    },

    async addResponsesApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/responses/channels/${channelId}/keys`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async removeResponsesApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/responses/channels/${channelId}/keys/remove`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async restoreResponsesApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/responses/channels/${channelId}/keys/restore`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async suspendResponsesApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/responses/channels/${channelId}/keys/suspend`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async resumeResponsesApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/responses/channels/${channelId}/keys/resume`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async restoreResponsesKeyModel(channelId: number, apiKey: string, model: string): Promise<void> {
      await client.request(`/responses/channels/${channelId}/keys/restore-model`, {
        method: 'POST',
        body: JSON.stringify({ apiKey, model })
      })
    },

    async getResponsesChannelModels(id: number, request: ChannelModelsRequest): Promise<ModelsResponse> {
      return client.request(`/responses/channels/${id}/models`, {
        method: 'POST',
        body: JSON.stringify(request)
      })
    },

    async moveResponsesApiKeyToTop(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/responses/channels/${channelId}/keys/move-to-top`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async moveResponsesApiKeyToBottom(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/responses/channels/${channelId}/keys/move-to-bottom`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    // 重新排序 Responses 渠道优先级
    async reorderResponsesChannels(order: number[], priorities?: number[]): Promise<void> {
      await client.request('/responses/channels/reorder', {
        method: 'POST',
        body: JSON.stringify(priorities ? { order, priorities } : { order })
      })
    },

    // 设置 Responses 渠道状态
    async setResponsesChannelStatus(channelId: number, status: ChannelStatus): Promise<void> {
      await client.request(`/responses/channels/${channelId}/status`, {
        method: 'PATCH',
        body: JSON.stringify({ status })
      })
    },

    // 恢复 Responses 熔断渠道
    async resumeResponsesChannel(channelId: number): Promise<ResumeChannelResponse> {
      return client.request(`/responses/channels/${channelId}/resume`, {
        method: 'POST'
      })
    },

    // 设置 Responses 渠道促销期
    async setResponsesChannelPromotion(channelId: number, durationSeconds: number): Promise<void> {
      await client.request(`/responses/channels/${channelId}/promotion`, {
        method: 'POST',
        body: JSON.stringify({ duration: durationSeconds })
      })
    }
  }
}

export type ChannelsResponsesApi = ReturnType<typeof createChannelsResponsesApi>
