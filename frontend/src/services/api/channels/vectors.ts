import type { ApiClient } from '../client'
import type { Channel, ChannelsResponse, PingResult, ResumeChannelResponse } from '../../types/channels'
import type { ChannelPlacement, ChannelStatus } from '../../types/common'
import type { ChannelModelsRequest, ModelsResponse } from '../../types/models'

export function createChannelsVectorsApi(client: ApiClient) {
  return {
    async getVectorsChannels(): Promise<ChannelsResponse> {
      return client.request('/vectors/channels')
    },

    async addVectorsChannel(
      channel: Omit<Channel, 'index' | 'latency' | 'status'>,
      placement?: ChannelPlacement
    ): Promise<void> {
      await client.request('/vectors/channels', {
        method: 'POST',
        body: JSON.stringify(placement ? { ...channel, placement } : channel)
      })
    },

    async updateVectorsChannel(id: number, channel: Partial<Channel>): Promise<void> {
      await client.request(`/vectors/channels/${id}`, {
        method: 'PUT',
        body: JSON.stringify(channel)
      })
    },

    async deleteVectorsChannel(id: number): Promise<void> {
      await client.request(`/vectors/channels/${id}`, {
        method: 'DELETE'
      })
    },

    async addVectorsApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/vectors/channels/${channelId}/keys`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async removeVectorsApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/vectors/channels/${channelId}/keys/remove`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async restoreVectorsApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/vectors/channels/${channelId}/keys/restore`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async suspendVectorsApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/vectors/channels/${channelId}/keys/suspend`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async resumeVectorsApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/vectors/channels/${channelId}/keys/resume`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async restoreVectorsKeyModel(channelId: number, apiKey: string, model: string): Promise<void> {
      await client.request(`/vectors/channels/${channelId}/keys/restore-model`, {
        method: 'POST',
        body: JSON.stringify({ apiKey, model })
      })
    },

    async moveVectorsApiKeyToTop(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/vectors/channels/${channelId}/keys/move-to-top`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async moveVectorsApiKeyToBottom(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/vectors/channels/${channelId}/keys/move-to-bottom`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async reorderVectorsChannels(order: number[], priorities?: number[]): Promise<void> {
      await client.request('/vectors/channels/reorder', {
        method: 'POST',
        body: JSON.stringify(priorities ? { order, priorities } : { order })
      })
    },

    async setVectorsChannelStatus(channelId: number, status: ChannelStatus): Promise<void> {
      await client.request(`/vectors/channels/${channelId}/status`, {
        method: 'PATCH',
        body: JSON.stringify({ status })
      })
    },

    async resumeVectorsChannel(channelId: number): Promise<ResumeChannelResponse> {
      return client.request(`/vectors/channels/${channelId}/resume`, {
        method: 'POST'
      })
    },

    async setVectorsChannelPromotion(channelId: number, durationSeconds: number): Promise<void> {
      await client.request(`/vectors/channels/${channelId}/promotion`, {
        method: 'POST',
        body: JSON.stringify({ duration: durationSeconds })
      })
    },

    async pingVectorsChannel(id: number): Promise<PingResult> {
      return client.request(`/vectors/ping/${id}`)
    },

    async pingAllVectorsChannels(): Promise<Array<{ id: number; name: string; latency: number; status: string }>> {
      const resp = await client.request('/vectors/ping')
      return (resp.channels || []).map((ch: { index: number; name: string; latency: number; success: boolean }) => ({
        id: ch.index,
        name: ch.name,
        latency: ch.latency,
        status: ch.success ? 'healthy' : 'error'
      }))
    },

    async getVectorsChannelModels(id: number, request: ChannelModelsRequest): Promise<ModelsResponse> {
      return client.request(`/vectors/channels/${id}/models`, {
        method: 'POST',
        body: JSON.stringify(request)
      })
    }
  }
}

export type ChannelsVectorsApi = ReturnType<typeof createChannelsVectorsApi>
