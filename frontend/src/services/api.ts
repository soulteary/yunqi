// Stable public facade. Business implementations depend only on ApiClient.
import { ApiClient } from './api/client'
import { createChannelsApi } from './api/channels/index'
import { createSystemApi } from './api/system'
import { createHealthApi } from './api/health'
import { createChannelManagementApi } from './api/channel-management'
import { createCapabilitiesApi } from './api/capabilities'
import { createMetricsApi } from './api/metrics'
import { createConversationsApi } from './api/conversations'
import { createSubscriptionsApi } from './api/subscriptions'
import { createCostsApi } from './api/costs'
import { createRoutingApi } from './api/routing'
import { createAccountsApi } from './api/accounts'

export * from './api-helpers'
export * from './api-types'

// This single composition defines both runtime methods and the public service type.
function createApiMethods(client: ApiClient) {
  return {
    ...createChannelsApi(client),
    ...createSystemApi(client),
    ...createHealthApi(client),
    ...createChannelManagementApi(client),
    ...createCapabilitiesApi(client),
    ...createMetricsApi(client),
    ...createConversationsApi(client),
    ...createSubscriptionsApi(client),
    ...createCostsApi(client),
    ...createRoutingApi(client),
    ...createAccountsApi(client)
  }
}

type ApiMethods = ReturnType<typeof createApiMethods>
// Object.assign installs the composed methods on each instance. Describe that
// constructor result once rather than merging a class and an interface.
const ApiServiceBase = class {
  constructor() {
    Object.assign(this, createApiMethods(new ApiClient()))
  }
} as new () => ApiMethods

export class ApiService extends ApiServiceBase {}

export const api = new ApiService()
export default api
