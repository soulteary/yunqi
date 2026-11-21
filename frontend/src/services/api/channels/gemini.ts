import type { ApiClient } from '../client'
import type { Channel, ChannelsResponse, PingResult, ResumeChannelResponse } from '../../types/channels'
import type { ChannelPlacement, ChannelStatus } from '../../types/common'
import type { ChannelModelsRequest, ModelsResponse } from '../../types/models'

export function createChannelsGeminiApi(client: ApiClient) {
  return {
    async getGeminiChannels(): Promise<ChannelsResponse> {
      return client.request('/gemini/channels')
    },

    async addGeminiChannel(
      channel: Omit<Channel, 'index' | 'latency' | 'status'>,
      placement?: ChannelPlacement
    ): Promise<void> {
      await client.request('/gemini/channels', {
        method: 'POST',
        body: JSON.stringify(placement ? { ...channel, placement } : channel)
      })
    },

    async updateGeminiChannel(id: number, channel: Partial<Channel>): Promise<void> {
      await client.request(`/gemini/channels/${id}`, {
        method: 'PUT',
        body: JSON.stringify(channel)
      })
    },

    async deleteGeminiChannel(id: number): Promise<void> {
      await client.request(`/gemini/channels/${id}`, {
        method: 'DELETE'
      })
    },

    async addGeminiApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/gemini/channels/${channelId}/keys`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async removeGeminiApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/gemini/channels/${channelId}/keys/remove`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async restoreGeminiApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/gemini/channels/${channelId}/keys/restore`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async suspendGeminiApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/gemini/channels/${channelId}/keys/suspend`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async resumeGeminiApiKey(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/gemini/channels/${channelId}/keys/resume`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async restoreGeminiKeyModel(channelId: number, apiKey: string, model: string): Promise<void> {
      await client.request(`/gemini/channels/${channelId}/keys/restore-model`, {
        method: 'POST',
        body: JSON.stringify({ apiKey, model })
      })
    },

    async moveGeminiApiKeyToTop(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/gemini/channels/${channelId}/keys/move-to-top`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async moveGeminiApiKeyToBottom(channelId: number, apiKey: string): Promise<void> {
      await client.request(`/gemini/channels/${channelId}/keys/move-to-bottom`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    async reorderGeminiChannels(order: number[], priorities?: number[]): Promise<void> {
      await client.request('/gemini/channels/reorder', {
        method: 'POST',
        body: JSON.stringify(priorities ? { order, priorities } : { order })
      })
    },

    async setGeminiChannelStatus(channelId: number, status: ChannelStatus): Promise<void> {
      await client.request(`/gemini/channels/${channelId}/status`, {
        method: 'PATCH',
        body: JSON.stringify({ status })
      })
    },

    // Gemini 恢复渠道（重置熔断并恢复被拉黑的 Key）
    async resumeGeminiChannel(channelId: number): Promise<ResumeChannelResponse> {
      return client.request(`/gemini/channels/${channelId}/resume`, {
        method: 'POST'
      })
    },

    async setGeminiChannelPromotion(channelId: number, durationSeconds: number): Promise<void> {
      await client.request(`/gemini/channels/${channelId}/promotion`, {
        method: 'POST',
        body: JSON.stringify({ duration: durationSeconds })
      })
    },

    async pingGeminiChannel(id: number): Promise<PingResult> {
      return client.request(`/gemini/ping/${id}`)
    },

    async pingAllGeminiChannels(): Promise<Array<{ id: number; name: string; latency: number; status: string }>> {
      const resp = await client.request('/gemini/ping')
      // 后端返回 { channels: [...] }，需要提取并转换字段名
      return (resp.channels || []).map((ch: { index: number; name: string; latency: number; success: boolean }) => ({
        id: ch.index,
        name: ch.name,
        latency: ch.latency,
        status: ch.success ? 'healthy' : 'error'
      }))
    },

    async getGeminiChannelModels(id: number, request: ChannelModelsRequest): Promise<ModelsResponse> {
      return client.request(`/gemini/channels/${id}/models`, {
        method: 'POST',
        body: JSON.stringify(request)
      })
    }
  }
}

export type ChannelsGeminiApi = ReturnType<typeof createChannelsGeminiApi>
