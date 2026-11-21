import type { ApiClient } from './client'
import type { ChannelSequenceEntry, ConversationsResponse } from '../types/conversations'

export function createConversationsApi(client: ApiClient) {
  return {
    async getConversations(kind?: string): Promise<ConversationsResponse> {
      const params = kind ? `?kind=${kind}` : ''
      return client.request(`/conversations${params}`)
    },

    async setConversationOverride(
      id: string,
      sequence: ChannelSequenceEntry[],
      duration?: number,
      subagentSequence?: ChannelSequenceEntry[],
      clearSubagentSequence = false
    ): Promise<void> {
      const body: Record<string, unknown> = { sequence }
      if (duration !== undefined) {
        body.duration = duration
      }
      if (subagentSequence && subagentSequence.length > 0) {
        body.subagentSequence = subagentSequence
      }
      if (clearSubagentSequence) {
        body.clearSubagentSequence = true
      }
      await client.request(`/conversations/${id}/override`, {
        method: 'POST',
        body: JSON.stringify(body)
      })
    },

    async removeConversationOverride(id: string): Promise<void> {
      await client.request(`/conversations/${id}/override`, {
        method: 'DELETE'
      })
    }
  }
}

export type ConversationsApi = ReturnType<typeof createConversationsApi>
