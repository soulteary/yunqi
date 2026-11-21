import type { ApiClient } from '../client'
import type { Channel, ChannelsResponse, PingResult, ResumeChannelResponse } from '../../types/channels'
import type { ChannelPlacement, ChannelStatus } from '../../types/common'
import type { ChannelModelsRequest, ModelsResponse } from '../../types/models'

export function createChannelsMessagesApi(client: ApiClient) {
  return {
    async getChannels(): Promise<ChannelsResponse> {
      return client.request('/messages/channels')
    },

    async addChannel(
      channel: Omit<Channel, 'index' | 'latency' | 'status'>,
      placement?: ChannelPlacement
    ): Promise<void> {
      await client.request('/messages/channels', {
        method: 'POST',
        body: JSON.stringify(placement ? { ...channel, placement } : channel)
      })
    },

    async updateChannel(id: number, channel: Partial<Channel>): Promise<void> {
      await client.request(`/messages/channels/${id}`, {
        method: 'PUT',
        body: JSON.stringify(channel)
      })
    },

    async deleteChannel(id: number): Promise<void> {
      await client.request(`/messages/channels/${id}`, {
        method: 'DELETE'
      })
    },

    async addApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/messages/channels/${channelId}/keys`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async removeApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/messages/channels/${channelId}/keys/remove`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async restoreApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/messages/channels/${channelId}/keys/restore`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async suspendApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/messages/channels/${channelId}/keys/suspend`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async resumeApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/messages/channels/${channelId}/keys/resume`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async restoreKeyModel(channelId: number, apiKey: string, model: string): Promise<void> {
      await client.request(`/messages/channels/${channelId}/keys/restore-model`, {
        method: 'POST',
        body: JSON.stringify({ apiKey, model })
      })
    },

    async pingChannel(id: number): Promise<PingResult> {
      return client.request(`/messages/ping/${id}`)
    },

    async pingAllChannels(): Promise<Array<{ id: number; name: string; latency: number; status: string }>> {
      return client.request('/messages/ping')
    },

    async getChannelModels(id: number, request: ChannelModelsRequest): Promise<ModelsResponse> {
      return client.request(`/messages/channels/${id}/models`, {
        method: 'POST',
        body: JSON.stringify(request)
      })
    },

    async moveApiKeyToTop(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/messages/channels/${channelId}/keys/move-to-top`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async moveApiKeyToBottom(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/messages/channels/${channelId}/keys/move-to-bottom`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    // 重新排序渠道优先级
    async reorderChannels(order: number[], priorities?: number[]): Promise<void> {
      await client.request('/messages/channels/reorder', {
        method: 'POST',
        body: JSON.stringify(priorities ? { order, priorities } : { order })
      })
    },

    // 设置渠道状态
    async setChannelStatus(channelId: number, status: ChannelStatus): Promise<void> {
      await client.request(`/messages/channels/${channelId}/status`, {
        method: 'PATCH',
        body: JSON.stringify({ status })
      })
    },

    // 恢复熔断渠道（重置错误计数）
    async resumeChannel(channelId: number): Promise<ResumeChannelResponse> {
      return client.request(`/messages/channels/${channelId}/resume`, {
        method: 'POST'
      })
    },

    // 设置 Messages 渠道促销期
    async setChannelPromotion(channelId: number, durationSeconds: number): Promise<void> {
      await client.request(`/messages/channels/${channelId}/promotion`, {
        method: 'POST',
        body: JSON.stringify({ duration: durationSeconds })
      })
    }
  }
}

export type ChannelsMessagesApi = ReturnType<typeof createChannelsMessagesApi>
