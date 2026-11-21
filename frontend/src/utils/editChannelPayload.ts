import type { Channel } from '../services/api'

export const EDIT_CHANNEL_PAYLOAD_KEYS = [
  'name', 'remark', 'serviceType', 'copilotProtocol', 'baseUrl', 'baseUrls', 'website', 'insecureSkipVerify',
  'lowQuality', 'injectDummyThoughtSignature', 'stripThoughtSignature', 'description',
  'apiKeys', 'apiKeyConfigs', 'modelCapabilities', 'embeddingCapabilities', 'defaultCapability', 'allowUnknownContext',
  'reasoningParamStyle', 'textVerbosity',
  'fastMode', 'customHeaders', 'proxyUrl', 'costMultiplier', 'maxGroupMultiplier', 'channelPaymentCurrency', 'channelPaymentAmount', 'channelCreditCurrency', 'channelCreditAmount', 'authHeader', 'requestTimeoutMs', 'responseHeaderTimeoutMs', 'streamFirstContentTimeoutMs', 'streamInactivityTimeoutMs', 'streamToolCallIdleTimeoutMs', 'routePrefix', 'supportedModels',
  'rateLimitRpm', 'rateLimitWindowMinutes', 'rateLimitMaxConcurrent', 'rateLimitAutoFromHeaders',
  'autoBlacklistBalance', 'normalizeMetadataUserId', 'stripBillingHeader', 'normalizeSystemRoleToTopLevel',
  'responsesReasoningFold', 'codexToolCompat', 'stripCodexClientTools', 'convertImageUrlToB64Json', 'tags', 'racing',
  'proxyPreferDirect', 'outboundNetwork', 'balanceCheck', 'rateLimitBurst', 'historicalImageTurnLimit', 'noVision',
] as const

export function extractEditChannelPayloadFields(channel: Channel): Record<string, unknown> {
  const result: Record<string, unknown> = {}
  for (const key of EDIT_CHANNEL_PAYLOAD_KEYS) {
    if (key in channel) {
      result[key] = channel[key as keyof Channel]
    }
  }
  return result
}

/** Saving contracts: undefined/omitted retains; null resets an optional override;
 * false, zero and empty containers are concrete updates. Account-owned identity
 * and credential fields use their dedicated save flow. */
export const ACCOUNT_CHANNEL_FIELDS = new Set<string>([
  'name', 'apiKeys', 'apiKeyConfigs', 'baseUrl', 'baseUrls', 'website', 'remark', 'responsesReasoningFold',
])
export const READ_ONLY_MANAGED_CHANNEL_FIELDS = new Set<string>(['serviceType', 'copilotProtocol'])
export const MANAGED_CHANNEL_PATCH_KEYS = EDIT_CHANNEL_PAYLOAD_KEYS.filter(key =>
  !ACCOUNT_CHANNEL_FIELDS.has(key) && !READ_ONLY_MANAGED_CHANNEL_FIELDS.has(key),
)

/** Provider templates derive these protocol settings at request time. Custom
 * managed accounts (generic) retain their explicit overrides. */
export const PROVIDER_AUTOMATIC_CHANNEL_FIELDS = new Set<string>([
  'reasoningParamStyle', 'fastMode', 'codexToolCompat', 'stripCodexClientTools',
  'convertImageUrlToB64Json', 'normalizeMetadataUserId', 'stripBillingHeader',
  'normalizeSystemRoleToTopLevel', 'injectDummyThoughtSignature', 'stripThoughtSignature',
  'noVision', 'historicalImageTurnLimit',
])
