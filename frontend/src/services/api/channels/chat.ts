import type { ApiClient } from '../client'
import type { Channel, ChannelsResponse, PingResult, ResumeChannelResponse } from '../../types/channels'
import type { ChannelPlacement, ChannelStatus } from '../../types/common'
import type { ChannelModelsRequest, ModelsResponse } from '../../types/models'

export function createChannelsChatApi(client: ApiClient) {
  return {
    async getChatChannels(): Promise<ChannelsResponse> {
      return client.request('/chat/channels')
    },

    async addChatChannel(
      channel: Omit<Channel, 'index' | 'latency' | 'status'>,
      placement?: ChannelPlacement
    ): Promise<void> {
      await client.request('/chat/channels', {
        method: 'POST',
        body: JSON.stringify(placement ? { ...channel, placement } : channel)
      })
    },

    async updateChatChannel(id: number, channel: Partial<Channel>): Promise<void> {
      await client.request(`/chat/channels/${id}`, {
        method: 'PUT',
        body: JSON.stringify(channel)
      })
    },

    async deleteChatChannel(id: number): Promise<void> {
      await client.request(`/chat/channels/${id}`, {
        method: 'DELETE'
      })
    },

    async addChatApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/chat/channels/${channelId}/keys`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async removeChatApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/chat/channels/${channelId}/keys/remove`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async restoreChatApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/chat/channels/${channelId}/keys/restore`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async suspendChatApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/chat/channels/${channelId}/keys/suspend`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async resumeChatApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/chat/channels/${channelId}/keys/resume`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async restoreChatKeyModel(channelId: number, apiKey: string, model: string): Promise<void> {
      await client.request(`/chat/channels/${channelId}/keys/restore-model`, {
        method: 'POST',
        body: JSON.stringify({ apiKey, model })
      })
    },

    async moveChatApiKeyToTop(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/chat/channels/${channelId}/keys/move-to-top`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async moveChatApiKeyToBottom(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/chat/channels/${channelId}/keys/move-to-bottom`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async reorderChatChannels(order: number[], priorities?: number[]): Promise<void> {
      await client.request('/chat/channels/reorder', {
        method: 'POST',
        body: JSON.stringify(priorities ? { order, priorities } : { order })
      })
    },

    async setChatChannelStatus(channelId: number, status: ChannelStatus): Promise<void> {
      await client.request(`/chat/channels/${channelId}/status`, {
        method: 'PATCH',
        body: JSON.stringify({ status })
      })
    },

    async resumeChatChannel(channelId: number): Promise<ResumeChannelResponse> {
      return client.request(`/chat/channels/${channelId}/resume`, {
        method: 'POST'
      })
    },

    async setChatChannelPromotion(channelId: number, durationSeconds: number): Promise<void> {
      await client.request(`/chat/channels/${channelId}/promotion`, {
        method: 'POST',
        body: JSON.stringify({ duration: durationSeconds })
      })
    },

    async pingChatChannel(id: number): Promise<PingResult> {
      return client.request(`/chat/ping/${id}`)
    },

    async pingAllChatChannels(): Promise<Array<{ id: number; name: string; latency: number; status: string }>> {
      const resp = await client.request('/chat/ping')
      return (resp.channels || []).map((ch: { index: number; name: string; latency: number; success: boolean }) => ({
        id: ch.index,
        name: ch.name,
        latency: ch.latency,
        status: ch.success ? 'healthy' : 'error'
      }))
    },

    async getChatChannelModels(id: number, request: ChannelModelsRequest): Promise<ModelsResponse> {
      return client.request(`/chat/channels/${id}/models`, {
        method: 'POST',
        body: JSON.stringify(request)
      })
    }
  }
}

export type ChannelsChatApi = ReturnType<typeof createChannelsChatApi>
