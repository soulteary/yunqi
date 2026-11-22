import { describe, expect, it } from 'vitest'
import { buildRequestPreviewDraft, requestPreviewSample } from './requestPreview'
describe('request preview boundary', () => {
  it('never sends stored credentials, identity or quotas in the draft', () => {
    expect(buildRequestPreviewDraft({ apiKeys: ['secret'], accountUid: 'private', rateLimitRpm: 5, fastMode: false, textVerbosity: 'high', stripBillingHeader: null, customHeaders: { 'X-Auth': 'draft' } })).toEqual({ fastMode: false, textVerbosity: 'high', stripBillingHeader: null, customHeaders: { 'X-Auth': 'draft' } })
  })
  it.each(['messages', 'chat', 'responses', 'gemini'])('builds valid %s samples', protocol => {
    const sample = JSON.parse(requestPreviewSample(protocol, 'sample-model'))
    if (protocol === 'gemini') expect(sample.contents).toHaveLength(1)
    else expect(sample.model).toBe('sample-model')
    if (protocol === 'messages') expect(sample.max_tokens).toBe(64)
  })
})
