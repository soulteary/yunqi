import type { ModelImportPolicy, ModelImportReview, ModelImportView } from '../types/model-import'
import type { RequestPreviewInput, RequestPreviewResult } from '../types/request-preview'
import type { ApiClient } from './client'
import type {
  CreateLogicalChannelRequest,
  DeleteLogicalChannelResponse,
  GroupModelPolicyResponse,
  LogicalChannelsResponse
} from '../types/channels'

export function createChannelManagementApi(client: ApiClient) {
  return {
    async removeModelImport(
      uid: string,
      protocol: string,
      revision: string,
      allowUnrestricted: boolean
    ): Promise<ModelImportView> {
      return client.request(
        `/channels/${encodeURIComponent(uid)}/model-import?protocol=${encodeURIComponent(protocol)}`,
        { method: 'DELETE', headers: { 'If-Match': revision }, body: JSON.stringify({ allowUnrestricted }) }
      )
    },
    async getModelImport(uid: string, protocol: string): Promise<ModelImportView> {
      return client.request(
        `/channels/${encodeURIComponent(uid)}/model-import?protocol=${encodeURIComponent(protocol)}`
      )
    },
    async setModelImportPolicy(
      uid: string,
      protocol: string,
      policy: ModelImportPolicy,
      revision: string
    ): Promise<ModelImportView> {
      return client.request(
        `/channels/${encodeURIComponent(uid)}/model-import/policy?protocol=${encodeURIComponent(protocol)}`,
        { method: 'PUT', headers: { 'If-Match': revision }, body: JSON.stringify(policy) }
      )
    },
    async previewModelImport(
      uid: string,
      protocol: string,
      ids: string[],
      revision: string
    ): Promise<ModelImportReview> {
      return client.request(
        `/channels/${encodeURIComponent(uid)}/model-import/preview?protocol=${encodeURIComponent(protocol)}`,
        { method: 'POST', headers: { 'If-Match': revision }, body: JSON.stringify({ models: ids.map(id => ({ id })) }) }
      )
    },
    async approveModelImport(
      uid: string,
      protocol: string,
      reviewId: string,
      allowEmpty: boolean,
      allowRemovals: boolean
    ): Promise<ModelImportView> {
      return client.request(
        `/channels/${encodeURIComponent(uid)}/model-import/approve?protocol=${encodeURIComponent(protocol)}`,
        { method: 'POST', body: JSON.stringify({ reviewId, allowEmpty, allowRemovals }) }
      )
    },
    async previewChannelRequest(uid: string, input: RequestPreviewInput): Promise<RequestPreviewResult> {
      return client.request(`/channels/${encodeURIComponent(uid)}/request-preview`, {
        method: 'POST',
        body: JSON.stringify(input)
      })
    },
    /** 获取统一渠道视图（渠道→key→endpoint→模型 + 共享能力） */
    async getChannelsV2(): Promise<{
      schemaVersion: number
      channels: Record<string, unknown>[]
      capabilities: Record<string, unknown>[]
    }> {
      return client.request('/channels')
    },

    /** 获取单个渠道视图 */
    async getChannelV2(
      uid: string
    ): Promise<{ channel: Record<string, unknown>; capabilities: Record<string, unknown>[]; revision?: string }> {
      return client.request(`/channels/${uid}`)
    },

    /** 创建渠道（统一入口，通过 kind 字段指定协议） */
    async addChannelV2(channel: {
      kind: string
      name?: string
      serviceType?: string
      baseUrl?: string
      baseUrls?: string[]
      apiKeys?: string[]
      placement?: string
      status?: string
    }): Promise<void> {
      await client.request('/channels', {
        method: 'POST',
        body: JSON.stringify(channel)
      })
    },

    /** 更新渠道（按 ChannelUID 寻址） */
    async updateChannelV2(
      uid: string,
      updates: Record<string, unknown>,
      expectedRevision?: string
    ): Promise<{ revision: string; persisted: boolean; changed: boolean; changedFields?: string[] }> {
      const result: { revision: string; persisted: boolean; changed: boolean; changedFields?: string[] } =
        await client.request(`/channels/${uid}`, {
          method: 'PUT',
          ...(expectedRevision ? { headers: { 'If-Match': expectedRevision } } : {}),
          body: JSON.stringify(updates)
        })
      if (!result?.persisted || !result.revision) throw new Error('服务端未确认配置已持久化')
      return result
    },

    /** 删除渠道（按 ChannelUID 寻址） */
    async deleteChannelV2(uid: string): Promise<void> {
      await client.request(`/channels/${uid}`, {
        method: 'DELETE'
      })
    },

    /** 添加 API Key（按 ChannelUID 寻址） */
    async addChannelKeyV2(uid: string, apiKey: string): Promise<void> {
      await client.request(`/channels/${uid}/keys`, {
        method: 'POST',
        body: JSON.stringify({ apiKey })
      })
    },

    /** 删除 API Key（按 ChannelUID + keyHash 寻址） */
    async removeChannelKeyV2(uid: string, keyHash: string): Promise<void> {
      await client.request(`/channels/${uid}/keys/${encodeURIComponent(keyHash)}`, {
        method: 'DELETE'
      })
    },

    async disableGroupModel(
      kind: 'messages' | 'chat' | 'responses' | 'gemini' | 'images' | 'vectors',
      channelId: number,
      apiKey: string,
      model: string
    ): Promise<GroupModelPolicyResponse> {
      return client.request(`/${kind}/channels/${channelId}/keys/group-model/disable`, {
        method: 'POST',
        body: JSON.stringify({ apiKey, model })
      })
    },

    async restoreGroupModel(
      kind: 'messages' | 'chat' | 'responses' | 'gemini' | 'images' | 'vectors',
      channelId: number,
      model: string,
      options: { apiKey?: string; quotaGroup?: string }
    ): Promise<GroupModelPolicyResponse> {
      return client.request(`/${kind}/channels/${channelId}/keys/group-model/restore`, {
        method: 'POST',
        body: JSON.stringify({
          model,
          apiKey: options.apiKey || undefined,
          quotaGroup: options.quotaGroup || undefined
        })
      })
    },

    /** 获取逻辑渠道列表 */
    async getLogicalChannels(kind?: string): Promise<LogicalChannelsResponse> {
      const qs = kind ? `?kind=${kind}` : ''
      return client.request(`/logical-channels${qs}`)
    },

    /** 创建逻辑渠道（一次创建多协议物理渠道） */
    async createLogicalChannel(
      req: CreateLogicalChannelRequest
    ): Promise<{ logicalChannelUid: string; channelUids: string[]; createdCount: number }> {
      return client.request('/logical-channels', {
        method: 'POST',
        body: JSON.stringify(req)
      })
    },

    /** 更新逻辑渠道 */
    async updateLogicalChannel(uid: string, updates: Record<string, unknown>): Promise<void> {
      await client.request(`/logical-channels/${encodeURIComponent(uid)}`, {
        method: 'PUT',
        body: JSON.stringify(updates)
      })
    },

    /** 删除逻辑渠道（一次删除旗下所有协议渠道） */
    async deleteLogicalChannel(uid: string): Promise<DeleteLogicalChannelResponse> {
      return client.request(`/logical-channels/${encodeURIComponent(uid)}`, {
        method: 'DELETE'
      })
    }
  }
}

export type ChannelManagementApi = ReturnType<typeof createChannelManagementApi>
