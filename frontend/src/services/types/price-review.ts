import type { ModelPricing } from './models'
export interface PriceReviewState {
  rollbackBlockedThrough?: string
  lastCheckError?: string
  lastCheckAt?: string
  signatureRequired: boolean
  sourceMaxAge: string
  sourceFutureSkew: string
  activePriceSource?: { versionId: string; dataVersion: string; sourceDigest?: string; signingKeyId?: string; signatureVerified: boolean; sourcePublishedAt?: string; sourceCheckedAt?: string }
  activeVersion: string
  activeDataVersion: string
  activeBundleHash: string
  rollbackScope: string
  pending?: {
    sourceKind: string
    policyError?: string
    sourcePublishedAt?: string
    sourceCheckedAt: string
    reviewId: string
    beforeBundleHash: string
    beforeVersion: string
    afterVersion: string
    dataVersion: string
    sourceUrl: string
    sourceDigest: string
    signatureVerified: boolean
    signingKeyId?: string
    thresholdPercent: number
    changeCount: number
    previewTruncated: boolean
    requiresAnomalyAcknowledgement: boolean
    changes: { model: string; before?: ModelPricing; after?: ModelPricing; anomaly: boolean; reasons?: string[] }[]
  }
  history: { versionId: string; dataVersion: string; bundleHash: string; at: string; actorId: string }[]
  audit: { at: string; actorId: string; action: string; versionId: string; sourceDigest?: string }[]
}
export interface PriceReviewResponse { persisted: boolean; state: PriceReviewState }
