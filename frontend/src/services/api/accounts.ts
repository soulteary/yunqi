import type { ApiClient } from './client'
import type {
  AutoAddChannelRequest,
  AutoAddChannelResponse,
  ChannelAutoStatusResponse,
  CompshareConsoleCookieResponse,
  DeepSeekAccountBalancesResponse,
  KimiConsoleTokenResponse,
  ManagedAccountsResponse,
  MiMoConsoleCookieResponse,
  UpdateManagedAccountResponse,
  VolcengineAccessKeyResponse,
  VolcenginePlanUsageRefreshResponse
} from '../types/accounts'

export function createAccountsApi(client: ApiClient) {
  return {
    /** 快速添加渠道（自动托管模式） */
    async autoAddChannel(kind: string, data: AutoAddChannelRequest): Promise<AutoAddChannelResponse> {
      return client.request(`/${kind}/channels/auto-add`, {
        method: 'POST',
        body: JSON.stringify(data)
      })
    },

    /** 原子更新自动托管账号名称与凭证集合。skipVerify 用于验证失败弹窗中用户确认"仍要保存"后的重试。 */
    async updateManagedAccount(
      accountUid: string,
      data: { name: string; apiKeys: string[]; baseUrls?: string[]; skipVerify?: boolean }
    ): Promise<UpdateManagedAccountResponse> {
      return client.request(`/accounts/${encodeURIComponent(accountUid)}`, {
        method: 'PUT',
        body: JSON.stringify(data)
      })
    },

    /** 获取自动托管账号及掩码凭证，不返回明文 Key。 */
    async getManagedAccounts(): Promise<ManagedAccountsResponse> {
      return client.request('/accounts')
    },

    /** 使用托管账号中已保存的 Key 查询 DeepSeek 官方余额，不回传明文 Key。 */
    async getDeepSeekAccountBalances(accountUid: string): Promise<DeepSeekAccountBalancesResponse> {
      return client.request(`/accounts/${encodeURIComponent(accountUid)}/deepseek-balance`)
    },

    /** 向账号增量添加一批凭证。 */
    async addManagedAccountCredentials(accountUid: string, apiKeys: string[]): Promise<UpdateManagedAccountResponse> {
      return client.request(`/accounts/${encodeURIComponent(accountUid)}/credentials`, {
        method: 'POST',
        body: JSON.stringify({ apiKeys })
      })
    },

    /** 原子增加和删除账号凭证，不回传已有明文 Key。 */
    async patchManagedAccountCredentials(
      accountUid: string,
      data: { addApiKeys: string[]; removeCredentialUids: string[] }
    ): Promise<UpdateManagedAccountResponse> {
      return client.request(`/accounts/${encodeURIComponent(accountUid)}/credentials`, {
        method: 'PATCH',
        body: JSON.stringify(data)
      })
    },

    /** 按稳定凭证 ID 删除账号中的单个 Key。 */
    async deleteManagedAccountCredential(
      accountUid: string,
      credentialUid: string
    ): Promise<UpdateManagedAccountResponse> {
      return client.request(
        `/accounts/${encodeURIComponent(accountUid)}/credentials/${encodeURIComponent(credentialUid)}`,
        {
          method: 'DELETE'
        }
      )
    },

    /** 为火山套餐推理 Key 绑定管控面 AK/SK，并自动识别 Agent/Coding Plan。 */
    async setVolcengineAccessKey(
      accountUid: string,
      credentialUid: string,
      data: { accessKeyId: string; secretAccessKey: string }
    ): Promise<VolcengineAccessKeyResponse> {
      return client.request(
        `/accounts/${encodeURIComponent(accountUid)}/credentials/${encodeURIComponent(credentialUid)}/volcengine-access-key`,
        {
          method: 'PUT',
          body: JSON.stringify(data)
        }
      )
    },

    /** 删除火山套餐推理 Key 绑定的管控面 AK/SK。 */
    async clearVolcengineAccessKey(accountUid: string, credentialUid: string): Promise<void> {
      await client.request(
        `/accounts/${encodeURIComponent(accountUid)}/credentials/${encodeURIComponent(credentialUid)}/volcengine-access-key`,
        {
          method: 'DELETE'
        }
      )
    },

    /** 刷新火山套餐用量快照（后端带 TTL 缓存）。 */
    async refreshVolcenginePlanUsage(
      accountUid: string,
      credentialUid: string
    ): Promise<VolcenginePlanUsageRefreshResponse> {
      return client.request(
        `/accounts/${encodeURIComponent(accountUid)}/credentials/${encodeURIComponent(credentialUid)}/volcengine-plan-usage/refresh`,
        {
          method: 'POST'
        }
      )
    },

    async setMiMoConsoleCookie(
      accountUid: string,
      credentialUid: string,
      data: { cookie: string; adoptCookieKey?: boolean }
    ): Promise<MiMoConsoleCookieResponse> {
      return client.request(
        `/accounts/${encodeURIComponent(accountUid)}/credentials/${encodeURIComponent(credentialUid)}/mimo-console-cookie`,
        {
          method: 'PUT',
          body: JSON.stringify(data)
        }
      )
    },

    async refreshMiMoConsoleCookie(
      accountUid: string,
      credentialUid: string
    ): Promise<{ tokenPlan: MiMoConsoleCookieResponse['tokenPlan'] }> {
      return client.request(
        `/accounts/${encodeURIComponent(accountUid)}/credentials/${encodeURIComponent(credentialUid)}/mimo-console-cookie/refresh`,
        {
          method: 'POST'
        }
      )
    },

    async clearMiMoConsoleCookie(accountUid: string, credentialUid: string): Promise<void> {
      await client.request(
        `/accounts/${encodeURIComponent(accountUid)}/credentials/${encodeURIComponent(credentialUid)}/mimo-console-cookie`,
        {
          method: 'DELETE'
        }
      )
    },

    async setCompshareConsoleCookie(
      accountUid: string,
      credentialUid: string,
      cookie: string
    ): Promise<CompshareConsoleCookieResponse> {
      return client.request(
        `/accounts/${encodeURIComponent(accountUid)}/credentials/${encodeURIComponent(credentialUid)}/compshare-console-cookie`,
        {
          method: 'PUT',
          body: JSON.stringify({ cookie })
        }
      )
    },

    async refreshCompshareConsoleCookie(
      accountUid: string,
      credentialUid: string
    ): Promise<{ plan: CompshareConsoleCookieResponse['plan'] }> {
      return client.request(
        `/accounts/${encodeURIComponent(accountUid)}/credentials/${encodeURIComponent(credentialUid)}/compshare-console-cookie/refresh`,
        {
          method: 'POST'
        }
      )
    },

    async clearCompshareConsoleCookie(accountUid: string, credentialUid: string): Promise<void> {
      await client.request(
        `/accounts/${encodeURIComponent(accountUid)}/credentials/${encodeURIComponent(credentialUid)}/compshare-console-cookie`,
        {
          method: 'DELETE'
        }
      )
    },

    async setKimiConsoleToken(
      accountUid: string,
      credentialUid: string,
      accessToken: string
    ): Promise<KimiConsoleTokenResponse> {
      return client.request(
        `/accounts/${encodeURIComponent(accountUid)}/credentials/${encodeURIComponent(credentialUid)}/kimi-console-token`,
        {
          method: 'PUT',
          body: JSON.stringify({ accessToken })
        }
      )
    },

    async refreshKimiConsoleToken(
      accountUid: string,
      credentialUid: string
    ): Promise<{ usage: KimiConsoleTokenResponse['usage'] }> {
      return client.request(
        `/accounts/${encodeURIComponent(accountUid)}/credentials/${encodeURIComponent(credentialUid)}/kimi-console-token/refresh`,
        {
          method: 'POST'
        }
      )
    },

    async clearKimiConsoleToken(accountUid: string, credentialUid: string): Promise<void> {
      await client.request(
        `/accounts/${encodeURIComponent(accountUid)}/credentials/${encodeURIComponent(credentialUid)}/kimi-console-token`,
        {
          method: 'DELETE'
        }
      )
    },

    /** 级联删除自动托管账号及其全部协议渠道。 */
    async deleteManagedAccount(accountUid: string): Promise<void> {
      await client.request(`/accounts/${encodeURIComponent(accountUid)}`, { method: 'DELETE' })
    },

    /** 查询渠道自动托管状态 */
    async getChannelAutoStatus(kind: string, channelId: number): Promise<ChannelAutoStatusResponse> {
      return client.request(`/${kind}/channels/${channelId}/auto-status`)
    }
  }
}

export type AccountsApi = ReturnType<typeof createAccountsApi>
