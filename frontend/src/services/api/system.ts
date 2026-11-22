import type { PriceReviewResponse } from '../types/price-review'
import type { NetworkInterface, NetworkDiagnosis, OutboundNetworkConfig } from '../types/network'
import type { ApiClient } from './client'
import type { PresetBundle, ProviderTemplatesResponse } from '../types/presets'
import type {
  ConsoleStatus,
  CodexAutoReviewMapping,
  CopilotDeviceCodeResponse,
  CopilotTokenResponse,
  CopilotUserResponse
} from '../types/system'

export function createSystemApi(client: ApiClient) {
  return {
    async getPriceReview(): Promise<PriceReviewResponse> { return client.request('/presets/prices') },
    async checkPresetPrices(): Promise<PriceReviewResponse> { return client.request('/presets/prices/check', { method: 'POST' }, 60000) },
    async approvePresetPrices(reviewId: string, allowAnomalies: boolean): Promise<PriceReviewResponse> { return client.request('/presets/prices/approve', { method: 'POST', body: JSON.stringify({ reviewId, allowAnomalies }) }) },
    async rollbackPresetPrices(bundleHash: string): Promise<PriceReviewResponse> { return client.request('/presets/prices/rollback', { method: 'POST', body: JSON.stringify({ bundleHash }) }) },
    async getNetworkInterfaces(): Promise<{ interfaces: NetworkInterface[] }> {
      return client.request('/network/interfaces')
    },
    async diagnoseNetwork(settings: { baseUrl?: string; channelUid?: string; proxyUrl?: string; proxyPreferDirect?: boolean; insecureSkipVerify?: boolean; outboundNetwork?: OutboundNetworkConfig | null }): Promise<{ results: NetworkDiagnosis[] }> {
      return client.request('/network/diagnose', { method: 'POST', body: JSON.stringify(settings) })
    },
    async getConsoleStatus(): Promise<ConsoleStatus> {
      return client.request('/console/status')
    },

    async getCodexAutoReviewMapping(): Promise<CodexAutoReviewMapping> {
      return client.request('/console/codex-auto-review')
    },

    async setCodexAutoReviewMapping(mapping: CodexAutoReviewMapping): Promise<CodexAutoReviewMapping> {
      return client.request('/console/codex-auto-review', { method: 'PUT', body: JSON.stringify(mapping) })
    },

    async requestCopilotDeviceCode(proxyUrl?: string): Promise<CopilotDeviceCodeResponse> {
      return client.request('/copilot/oauth/device/code', {
        method: 'POST',
        body: JSON.stringify({ proxyUrl: proxyUrl?.trim() || undefined })
      })
    },

    async pollCopilotAccessToken(deviceCode: string, proxyUrl?: string): Promise<CopilotTokenResponse> {
      return client.request('/copilot/oauth/token', {
        method: 'POST',
        body: JSON.stringify({ deviceCode, proxyUrl: proxyUrl?.trim() || undefined })
      })
    },

    async verifyCopilotAccessToken(accessToken: string, proxyUrl?: string): Promise<CopilotUserResponse> {
      return client.request('/copilot/oauth/verify', {
        method: 'POST',
        body: JSON.stringify({ accessToken, proxyUrl: proxyUrl?.trim() || undefined })
      })
    },

    async diagnoseCopilotChannel(channelId: number, accessToken?: string): Promise<Record<string, unknown>> {
      return client.request(`/responses/channels/${channelId}/copilot/diagnose`, {
        method: 'POST',
        body: JSON.stringify({ accessToken })
      })
    },

    // 获取熔断器运行时配置
    async getCircuitBreaker(): Promise<{
      windowSize: number
      failureThreshold: number
      consecutiveFailuresThreshold: number
      requestTimeoutMs: number
      responseHeaderTimeoutMs: number
      streamFirstContentTimeoutMs: number
      streamInactivityTimeoutMs: number
      streamToolCallIdleTimeoutMs: number
    }> {
      return client.request('/settings/circuit-breaker')
    },

    // 更新熔断器运行时配置（partial update）
    async setCircuitBreaker(params: {
      windowSize?: number
      failureThreshold?: number
      consecutiveFailuresThreshold?: number
      requestTimeoutMs?: number
      responseHeaderTimeoutMs?: number
      streamFirstContentTimeoutMs?: number
      streamInactivityTimeoutMs?: number
      streamToolCallIdleTimeoutMs?: number
    }): Promise<unknown> {
      return client.request('/settings/circuit-breaker', {
        method: 'PUT',
        body: JSON.stringify(params)
      })
    },

    async getPresets(): Promise<PresetBundle> {
      return client.request('/presets')
    },

    /** 获取内置 provider 模板（模板化添加：选 provider + 输 key） */
    async getProviderTemplates(): Promise<ProviderTemplatesResponse> {
      return client.request('/channels/provider-templates')
    }
  }
}

export type SystemApi = ReturnType<typeof createSystemApi>
