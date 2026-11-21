import type { ApiClient } from '../client'
import type { Channel, ChannelsResponse, PingResult, ResumeChannelResponse } from '../../types/channels'
import type { ChannelPlacement, ChannelStatus } from '../../types/common'
import type { ChannelModelsRequest, ModelsResponse } from '../../types/models'

export function createChannelsImagesApi(client: ApiClient) {
  return {
    async getImagesChannels(): Promise<ChannelsResponse> {
      return client.request('/images/channels')
    },

    async addImagesChannel(
      channel: Omit<Channel, 'index' | 'latency' | 'status'>,
      placement?: ChannelPlacement
    ): Promise<void> {
      await client.request('/images/channels', {
        method: 'POST',
        body: JSON.stringify(placement ? { ...channel, placement } : channel)
      })
    },

    async updateImagesChannel(id: number, channel: Partial<Channel>): Promise<void> {
      await client.request(`/images/channels/${id}`, {
        method: 'PUT',
        body: JSON.stringify(channel)
      })
    },

    async deleteImagesChannel(id: number): Promise<void> {
      await client.request(`/images/channels/${id}`, {
        method: 'DELETE'
      })
    },

    async addImagesApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/images/channels/${channelId}/keys`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async removeImagesApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/images/channels/${channelId}/keys/${encodeURIComponent(apiKey)}`, {
        method: 'DELETE'
      })
    },

    async restoreImagesApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/images/channels/${channelId}/keys/restore`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async suspendImagesApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/images/channels/${channelId}/keys/suspend`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async resumeImagesApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/images/channels/${channelId}/keys/resume`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async restoreImagesKeyModel(channelId: number, apiKey: string, model: string): Promise<void> {
      await client.request(`/images/channels/${channelId}/keys/restore-model`, {
        method: 'POST',
        body: JSON.stringify({ apiKey, model })
      })
    },

    async moveImagesApiKeyToTop(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/images/channels/${channelId}/keys/${encodeURIComponent(apiKey)}/top`, {
        method: 'POST'
      })
    },

    async moveImagesApiKeyToBottom(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/images/channels/${channelId}/keys/${encodeURIComponent(apiKey)}/bottom`, {
        method: 'POST'
      })
    },

    async reorderImagesChannels(order: number[], priorities?: number[]): Promise<void> {
      await client.request('/images/channels/reorder', {
        method: 'POST',
        body: JSON.stringify(priorities ? { order, priorities } : { order })
      })
    },

    async setImagesChannelStatus(channelId: number, status: ChannelStatus): Promise<void> {
      await client.request(`/images/channels/${channelId}/status`, {
        method: 'PATCH',
        body: JSON.stringify({ status })
      })
    },

    async resumeImagesChannel(channelId: number): Promise<ResumeChannelResponse> {
      return client.request(`/images/channels/${channelId}/resume`, {
        method: 'POST'
      })
    },

    async setImagesChannelPromotion(channelId: number, durationSeconds: number): Promise<void> {
      await client.request(`/images/channels/${channelId}/promotion`, {
        method: 'POST',
        body: JSON.stringify({ duration: durationSeconds })
      })
    },

    async pingImagesChannel(id: number): Promise<PingResult> {
      return client.request(`/images/ping/${id}`)
    },

    async pingAllImagesChannels(): Promise<Array<{ id: number; name: string; latency: number; status: string }>> {
      const resp = await client.request('/images/ping')
      return (resp.channels || []).map((ch: { index: number; name: string; latency: number; success: boolean }) => ({
        id: ch.index,
        name: ch.name,
        latency: ch.latency,
        status: ch.success ? 'healthy' : 'error'
      }))
    },

    async getImagesChannelModels(id: number, request: ChannelModelsRequest): Promise<ModelsResponse> {
      return client.request(`/images/channels/${id}/models`, {
        method: 'POST',
        body: JSON.stringify(request)
      })
    }
  }
}

export type ChannelsImagesApi = ReturnType<typeof createChannelsImagesApi>
