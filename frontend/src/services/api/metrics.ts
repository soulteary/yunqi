import type { ApiClient } from './client'
import type { ChannelDashboardResponse, LlmChannelDashboardResponse } from '../types/channels'
import type {
  ChannelKeyMetricsHistoryResponse,
  ChannelLogsResponse,
  ChannelMetrics,
  GlobalStatsHistoryResponse,
  MetricsHistoryResponse,
  ModelStatsHistoryResponse,
  SchedulerStatsResponse
} from '../types/metrics'
import type { SchedulerDiagnoseRequest, SchedulerDiagnoseResponse } from '../types/routing'

export function createMetricsApi(client: ApiClient) {
  return {
    // 获取渠道指标
    async getChannelMetrics(): Promise<ChannelMetrics[]> {
      return client.request('/messages/channels/metrics')
    },

    // 获取调度器统计信息
    async getSchedulerStats(
      type?: 'messages' | 'responses' | 'gemini' | 'chat' | 'images' | 'vectors'
    ): Promise<SchedulerStatsResponse> {
      // All managed channel groups share the scheduler stats endpoint via ?type=.
      const query = type && type !== 'messages' ? `?type=${type}` : ''
      return client.request(`/messages/channels/scheduler/stats${query}`)
    },

    // 获取渠道仪表盘数据（合并 channels + metrics + stats）
    async getChannelDashboard(
      type: 'messages' | 'responses' | 'gemini' | 'chat' | 'images' | 'vectors' = 'messages'
    ): Promise<ChannelDashboardResponse> {
      const query = type !== 'messages' ? `?type=${type}` : ''
      return client.request(`/messages/channels/dashboard${query}`)
    },

    // 获取统一 LLM 渠道仪表盘，避免前端分别轮询四种协议。
    async getLlmChannelDashboard(): Promise<LlmChannelDashboardResponse> {
      return client.request('/messages/channels/dashboard?type=llm')
    },

    // 获取 Responses 渠道指标
    async getResponsesChannelMetrics(): Promise<ChannelMetrics[]> {
      return client.request('/responses/channels/metrics')
    },

    // 获取 Messages 渠道历史指标（用于时间序列图表）
    async getChannelMetricsHistory(duration: string = '24h', interval?: string): Promise<MetricsHistoryResponse[]> {
      return client.request(`/messages/channels/metrics/history?${client.historyQuery(duration, interval)}`)
    },

    // 获取 Responses 渠道历史指标
    async getResponsesChannelMetricsHistory(
      duration: string = '24h',
      interval?: string
    ): Promise<MetricsHistoryResponse[]> {
      return client.request(`/responses/channels/metrics/history?${client.historyQuery(duration, interval)}`)
    },

    // 获取 Messages 渠道 Key 级别历史指标（用于 Key 趋势图表）
    async getChannelKeyMetricsHistory(
      channelId: number,
      duration: string = '6h',
      interval?: string
    ): Promise<ChannelKeyMetricsHistoryResponse> {
      return client.request(
        `/messages/channels/${channelId}/keys/metrics/history?${client.historyQuery(duration, interval)}`
      )
    },

    // 获取 Responses 渠道 Key 级别历史指标
    async getResponsesChannelKeyMetricsHistory(
      channelId: number,
      duration: string = '6h',
      interval?: string
    ): Promise<ChannelKeyMetricsHistoryResponse> {
      return client.request(
        `/responses/channels/${channelId}/keys/metrics/history?${client.historyQuery(duration, interval)}`
      )
    },

    // 获取 Messages 全局统计历史
    async getMessagesGlobalStats(duration: string = '24h', interval?: string): Promise<GlobalStatsHistoryResponse> {
      return client.request(`/messages/global/stats/history?${client.historyQuery(duration, interval)}`)
    },

    // 获取 Responses 全局统计历史
    async getResponsesGlobalStats(duration: string = '24h', interval?: string): Promise<GlobalStatsHistoryResponse> {
      return client.request(`/responses/global/stats/history?${client.historyQuery(duration, interval)}`)
    },

    async getModelStatsHistory(
      type: 'messages' | 'responses' | 'gemini' | 'chat' | 'images' | 'vectors',
      duration: string = '24h'
    ): Promise<ModelStatsHistoryResponse> {
      return client.request(`/${type}/models/stats/history?duration=${duration}`)
    },

    async getChannelLogs(
      type: 'messages' | 'responses' | 'gemini' | 'chat' | 'images' | 'vectors',
      channelId: number
    ): Promise<ChannelLogsResponse> {
      return client.request(`/${type}/channels/${channelId}/logs`)
    },

    async diagnoseSchedulerSelection(
      type: 'messages' | 'responses' | 'gemini' | 'chat' | 'images' | 'vectors',
      payload: SchedulerDiagnoseRequest
    ): Promise<SchedulerDiagnoseResponse> {
      return client.request(`/${type}/channels/scheduler/diagnose`, {
        method: 'POST',
        body: JSON.stringify(payload)
      })
    },

    async getChatChannelMetrics(): Promise<ChannelMetrics[]> {
      return client.request('/chat/channels/metrics')
    },

    async getChatChannelMetricsHistory(duration: string = '24h', interval?: string): Promise<MetricsHistoryResponse[]> {
      return client.request(`/chat/channels/metrics/history?${client.historyQuery(duration, interval)}`)
    },

    async getChatChannelKeyMetricsHistory(
      channelId: number,
      duration: string = '6h',
      interval?: string
    ): Promise<ChannelKeyMetricsHistoryResponse> {
      return client.request(
        `/chat/channels/${channelId}/keys/metrics/history?${client.historyQuery(duration, interval)}`
      )
    },

    async getChatGlobalStats(duration: string = '24h', interval?: string): Promise<GlobalStatsHistoryResponse> {
      return client.request(`/chat/global/stats/history?${client.historyQuery(duration, interval)}`)
    },

    async getImagesChannelMetrics(): Promise<ChannelMetrics[]> {
      return client.request('/images/channels/metrics')
    },

    async getImagesChannelMetricsHistory(
      duration: string = '24h',
      interval?: string
    ): Promise<MetricsHistoryResponse[]> {
      return client.request(`/images/channels/metrics/history?${client.historyQuery(duration, interval)}`)
    },

    async getImagesChannelKeyMetricsHistory(
      channelId: number,
      duration: string = '6h',
      interval?: string
    ): Promise<ChannelKeyMetricsHistoryResponse> {
      return client.request(
        `/images/channels/${channelId}/keys/metrics/history?${client.historyQuery(duration, interval)}`
      )
    },

    async getImagesGlobalStats(duration: string = '24h', interval?: string): Promise<GlobalStatsHistoryResponse> {
      return client.request(`/images/global/stats/history?${client.historyQuery(duration, interval)}`)
    },

    async getVectorsChannelMetrics(): Promise<ChannelMetrics[]> {
      return client.request('/vectors/channels/metrics')
    },

    async getVectorsChannelMetricsHistory(
      duration: string = '24h',
      interval?: string
    ): Promise<MetricsHistoryResponse[]> {
      return client.request(`/vectors/channels/metrics/history?${client.historyQuery(duration, interval)}`)
    },

    async getVectorsChannelKeyMetricsHistory(
      channelId: number,
      duration: string = '6h',
      interval?: string
    ): Promise<ChannelKeyMetricsHistoryResponse> {
      return client.request(
        `/vectors/channels/${channelId}/keys/metrics/history?${client.historyQuery(duration, interval)}`
      )
    },

    async getVectorsGlobalStats(duration: string = '24h', interval?: string): Promise<GlobalStatsHistoryResponse> {
      return client.request(`/vectors/global/stats/history?${client.historyQuery(duration, interval)}`)
    },

    async getGeminiChannelMetrics(): Promise<ChannelMetrics[]> {
      return client.request('/gemini/channels/metrics')
    },

    // 获取 Gemini 渠道历史指标
    async getGeminiChannelMetricsHistory(
      duration: string = '24h',
      interval?: string
    ): Promise<MetricsHistoryResponse[]> {
      return client.request(`/gemini/channels/metrics/history?${client.historyQuery(duration, interval)}`)
    },

    // 获取 Gemini 渠道 Key 级别历史指标
    async getGeminiChannelKeyMetricsHistory(
      channelId: number,
      duration: string = '6h',
      interval?: string
    ): Promise<ChannelKeyMetricsHistoryResponse> {
      return client.request(
        `/gemini/channels/${channelId}/keys/metrics/history?${client.historyQuery(duration, interval)}`
      )
    },

    // 获取 Gemini 全局统计历史
    async getGeminiGlobalStats(duration: string = '24h', interval?: string): Promise<GlobalStatsHistoryResponse> {
      return client.request(`/gemini/global/stats/history?${client.historyQuery(duration, interval)}`)
    }
  }
}

export type MetricsApi = ReturnType<typeof createMetricsApi>
