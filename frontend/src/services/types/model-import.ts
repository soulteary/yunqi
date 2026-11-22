export interface ModelImportRule {
  pattern: string
  replacement: string
}
export interface ModelImportPolicy {
  enabled: boolean
  include?: string[]
  exclude?: string[]
  stripPrefix?: string
  stripSuffix?: string
  case?: '' | 'lower' | 'upper'
  rules?: ModelImportRule[]
}
export interface ModelEndpointMetadata {
  type: string
  source: string
  verified: boolean
}
export interface ImportedModel {
  id: string
  upstreamId: string
  endpoints?: ModelEndpointMetadata[]
}
export interface ModelImportReview {
  id: string
  baseRevision: string
  observedAt: string
  source: string
  models: ImportedModel[]
  diff: { added: string[]; removed: string[]; changed: string[] }
  previousSupportedModels?: string[]
}
export interface ModelImportView {
  unrestrictedOnRemove?: boolean
  configured: boolean
  channelUid: string
  revision: string
  state: {
    policy: ModelImportPolicy
    approved?: ImportedModel[]
    pending?: ModelImportReview
    audit?: Array<{ at: string; actorId: string; action: string; reviewId?: string; modelCount: number }>
  }
}
