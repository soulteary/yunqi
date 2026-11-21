import type { ApiClient } from './client'
import type {
  CapabilitySnapshot,
  CapabilityTestJob,
  CapabilityTestJobStartResponse,
  CapabilityTestResult,
  ChannelDiscoveryFastRequest,
  ChannelDiscoveryFastResponse,
  ChannelDiscoveryRequest,
  ChannelDiscoveryResponse,
  CompatDiagnoseResult,
  StartCapabilityTestOptions
} from '../types/capabilities'

export function createCapabilitiesApi(client: ApiClient) {
  return {
    async diagnoseChannelCompat(
      type: 'messages' | 'chat' | 'gemini' | 'responses',
      id: number
    ): Promise<CompatDiagnoseResult> {
      return client.request(`/${type}/channels/${id}/compat-diagnose`, { method: 'POST', body: '{}' })
    },

    async discoverChannelConfig(request: ChannelDiscoveryRequest): Promise<ChannelDiscoveryResponse> {
      return client.request('/channel-discovery', {
        method: 'POST',
        body: JSON.stringify(request)
      })
    },

    /** 快速探活：仅探一个真实模型以定 primaryKind，不做全量协议/能力探测。 */
    async discoverChannelConfigFast(request: ChannelDiscoveryFastRequest): Promise<ChannelDiscoveryFastResponse> {
      return client.request('/channel-discovery-fast', {
        method: 'POST',
        body: JSON.stringify(request)
      })
    },

    async startChannelCapabilityTest(
      type: 'messages' | 'chat' | 'gemini' | 'responses',
      id: number,
      options: StartCapabilityTestOptions = {}
    ): Promise<CapabilityTestJobStartResponse> {
      const body: {
        targetProtocols: string[]
        timeout: number
        previousJobId?: string
        rpm?: number
        sourceTab?: string
        models?: string[]
        useChannelModels?: boolean
      } = {
        targetProtocols: options.targetProtocols?.length
          ? options.targetProtocols
          : ['messages', 'responses', 'chat', 'gemini'],
        timeout: 10000,
        rpm: options.rpm
      }
      if (options.previousJobId) {
        body.previousJobId = options.previousJobId
      }
      if (options.sourceTab) {
        body.sourceTab = options.sourceTab
      }
      if (options.models?.length) {
        body.models = options.models
      }
      if (options.useChannelModels) {
        body.useChannelModels = true
      }
      return client.request(`/${type}/channels/${id}/capability-test`, {
        method: 'POST',
        body: JSON.stringify(body)
      })
    },

    async getChannelCapabilitySnapshot(
      type: 'messages' | 'chat' | 'gemini' | 'responses',
      id: number,
      sourceTab?: string
    ): Promise<CapabilitySnapshot> {
      const url = sourceTab
        ? `/${type}/channels/${id}/capability-snapshot?sourceTab=${sourceTab}`
        : `/${type}/channels/${id}/capability-snapshot`
      return client.request(url)
    },

    async getChannelCapabilityTestStatus(
      type: 'messages' | 'chat' | 'gemini' | 'responses',
      id: number,
      jobId: string
    ): Promise<CapabilityTestJob> {
      return client.request(`/${type}/channels/${id}/capability-test/${jobId}`)
    },

    async cancelCapabilityTest(
      type: 'messages' | 'chat' | 'gemini' | 'responses',
      id: number,
      jobId: string
    ): Promise<void> {
      await client.request(`/${type}/channels/${id}/capability-test/${jobId}`, {
        method: 'DELETE'
      })
    },

    async retryCapabilityTestModel(
      type: 'messages' | 'chat' | 'gemini' | 'responses',
      id: number,
      jobId: string,
      protocol: string,
      model: string
    ): Promise<void> {
      await client.request(`/${type}/channels/${id}/capability-test/${jobId}/retry`, {
        method: 'POST',
        body: JSON.stringify({ protocol, model })
      })
    },

    async testChannelCapability(
      type: 'messages' | 'chat' | 'gemini' | 'responses',
      id: number
    ): Promise<CapabilityTestResult> {
      return client.request(`/${type}/channels/${id}/capability-test`, {
        method: 'POST',
        body: JSON.stringify({
          targetProtocols: ['messages', 'responses', 'chat', 'gemini'],
          timeout: 10000
        })
      })
    }
  }
}

export type CapabilitiesApi = ReturnType<typeof createCapabilitiesApi>
