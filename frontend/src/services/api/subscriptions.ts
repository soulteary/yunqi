import type { ApiClient } from './client'
import type {
  BillingTermsPatch,
  BillingTermsResponse,
  NewApiAccountCreateRequest,
  NewApiAccountCredentialsUpdateRequest,
  NewApiAccountItem,
  NewApiAccountListResponse,
  NewApiCredentialsUpdateRequest,
  NewApiProvisionRequest,
  NewApiProvisionResponse,
  NewApiVerifyRequest,
  NewApiVerifyResponse,
  SubscriptionCreateRequest,
  SubscriptionItem,
  SubscriptionRefreshResponse,
  SubscriptionUpdateRequest,
  SubscriptionsListResponse
} from '../types/subscriptions'

export function createSubscriptionsApi(client: ApiClient) {
  return {
    async getSubscriptions(): Promise<SubscriptionsListResponse> {
      return client.request('/subscriptions')
    },

    async getSubscription(uid: string): Promise<SubscriptionItem> {
      return client.request(`/subscriptions/${encodeURIComponent(uid)}`)
    },

    async createSubscription(data: SubscriptionCreateRequest): Promise<SubscriptionItem> {
      return client.request('/subscriptions', {
        method: 'POST',
        body: JSON.stringify(data)
      })
    },

    async updateSubscription(uid: string, data: SubscriptionUpdateRequest): Promise<SubscriptionItem> {
      return client.request(`/subscriptions/${encodeURIComponent(uid)}`, {
        method: 'PUT',
        body: JSON.stringify(data)
      })
    },

    async deleteSubscription(uid: string): Promise<void> {
      await client.request(`/subscriptions/${encodeURIComponent(uid)}`, {
        method: 'DELETE'
      })
    },

    async linkSubscriptionChannel(uid: string, channelUid: string): Promise<SubscriptionItem> {
      return client.request(`/subscriptions/${encodeURIComponent(uid)}/link`, {
        method: 'POST',
        body: JSON.stringify({ channelUid })
      })
    },

    async unlinkSubscriptionChannel(uid: string, channelUid: string): Promise<SubscriptionItem> {
      return client.request(`/subscriptions/${encodeURIComponent(uid)}/unlink`, {
        method: 'POST',
        body: JSON.stringify({ channelUid })
      })
    },

    async refreshSubscription(uid: string): Promise<SubscriptionRefreshResponse> {
      return client.request(`/subscriptions/${encodeURIComponent(uid)}/refresh`, {
        method: 'POST'
      })
    },

    async patchSubscriptionBillingTerms(uid: string, data: BillingTermsPatch): Promise<BillingTermsResponse> {
      return client.request(`/subscriptions/${encodeURIComponent(uid)}/billing-terms`, {
        method: 'PATCH',
        body: JSON.stringify(data)
      })
    },

    /** 校验 new-api 令牌并预览账户/分组/模型（不落库） */
    async verifyNewApiSubscription(data: NewApiVerifyRequest): Promise<NewApiVerifyResponse> {
      return client.request('/subscriptions/newapi/verify', {
        method: 'POST',
        body: JSON.stringify(data)
      })
    },

    /** 完整接入 new-api：建 profile + 建 key + 建渠道 + 触发 Discovery */
    async provisionNewApiSubscription(data: NewApiProvisionRequest): Promise<NewApiProvisionResponse> {
      return client.request('/subscriptions/newapi/provision', {
        method: 'POST',
        body: JSON.stringify(data)
      })
    },

    /** 获取 new-api 订阅下的账号列表 */
    /** 更新 new-api 订阅主账号凭证；响应仅返回脱敏 token */
    async updateNewApiCredentials(uid: string, data: NewApiCredentialsUpdateRequest): Promise<SubscriptionItem> {
      return client.request(`/subscriptions/${encodeURIComponent(uid)}/newapi-credentials`, {
        method: 'PATCH',
        body: JSON.stringify(data)
      })
    },

    async getSubscriptionAccounts(uid: string): Promise<NewApiAccountListResponse> {
      return client.request(`/subscriptions/${encodeURIComponent(uid)}/accounts`)
    },

    /** 为 new-api 订阅添加新账号 */
    async addSubscriptionAccount(uid: string, data: NewApiAccountCreateRequest): Promise<NewApiAccountItem> {
      return client.request(`/subscriptions/${encodeURIComponent(uid)}/accounts`, {
        method: 'POST',
        body: JSON.stringify(data)
      })
    },

    /** 删除 new-api 订阅下的账号 */
    async deleteSubscriptionAccount(uid: string, accountUid: string): Promise<void> {
      await client.request(`/subscriptions/${encodeURIComponent(uid)}/accounts/${encodeURIComponent(accountUid)}`, {
        method: 'DELETE'
      })
    },

    /** 删除 new-api 订阅的主账号（清空订阅凭证并剔除其自动接入 key；订阅本体保留） */
    async deleteSubscriptionPrimaryAccount(uid: string): Promise<void> {
      await client.request(`/subscriptions/${encodeURIComponent(uid)}/accounts/primary`, {
        method: 'DELETE'
      })
    },

    /** 刷新 new-api 账号余额 */
    async refreshSubscriptionAccount(uid: string, accountUid: string): Promise<NewApiAccountItem> {
      return client.request(
        `/subscriptions/${encodeURIComponent(uid)}/accounts/${encodeURIComponent(accountUid)}/refresh`,
        {
          method: 'POST'
        }
      )
    },

    /** 更新 new-api 子账号凭证（落库前先经站点验证）；响应仅返回脱敏 token */
    async updateSubscriptionAccountCredentials(
      uid: string,
      accountUid: string,
      data: NewApiAccountCredentialsUpdateRequest
    ): Promise<NewApiAccountItem> {
      return client.request(
        `/subscriptions/${encodeURIComponent(uid)}/accounts/${encodeURIComponent(accountUid)}/credentials`,
        {
          method: 'PATCH',
          body: JSON.stringify(data)
        }
      )
    }
  }
}

export type SubscriptionsApi = ReturnType<typeof createSubscriptionsApi>
