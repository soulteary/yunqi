import type { ApiClient } from './client'
import type {
  CostReportResponse,
  ExchangeRatesReplaceRequest,
  ExchangeRatesResponse,
  KeyMultiplierPatch,
  KeyMultiplierResponse
} from '../types/costs'

export function createCostsApi(client: ApiClient) {
  return {
    async getExchangeRates(): Promise<ExchangeRatesResponse> {
      return client.request('/autopilot/cost/exchange-rates')
    },

    async replaceExchangeRates(data: ExchangeRatesReplaceRequest): Promise<ExchangeRatesResponse> {
      return client.request('/autopilot/cost/exchange-rates', {
        method: 'PUT',
        body: JSON.stringify(data)
      })
    },

    async patchKeyMultiplier(
      kind: 'messages' | 'chat' | 'responses' | 'gemini' | 'images' | 'vectors',
      channelUid: string,
      keyUid: string,
      data: KeyMultiplierPatch
    ): Promise<KeyMultiplierResponse> {
      return client.request(
        `/${kind}/channels/${encodeURIComponent(channelUid)}/keys/${encodeURIComponent(keyUid)}/multiplier`,
        {
          method: 'PATCH',
          body: JSON.stringify(data)
        }
      )
    },

    /** 获取成本报表（按 user/model/key 分组） */
    async getCostReport(
      groupBy: string = 'user',
      duration: string = '7d',
      apiType: string = 'messages'
    ): Promise<CostReportResponse> {
      return client.request(`/reports/cost?groupBy=${groupBy}&duration=${duration}&type=${apiType}`)
    }
  }
}

export type CostsApi = ReturnType<typeof createCostsApi>
