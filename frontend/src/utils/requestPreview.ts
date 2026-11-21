const fields = ['serviceType', 'authHeader', 'customHeaders', 'reasoningParamStyle', 'textVerbosity', 'fastMode', 'codexToolCompat', 'stripCodexClientTools', 'normalizeMetadataUserId', 'stripBillingHeader', 'normalizeSystemRoleToTopLevel', 'injectDummyThoughtSignature', 'stripThoughtSignature', 'responsesClaudePrefixCache']
export function buildRequestPreviewDraft(form: object): Record<string, unknown> {
  return Object.fromEntries(Object.entries(form).filter(([key, value]) => fields.includes(key) && value !== undefined))
}
export function requestPreviewSample(protocol: string, model: string): string {
  if (protocol === 'gemini') return JSON.stringify({ contents: [{ role: 'user', parts: [{ text: 'Preview only' }] }] }, null, 2)
  if (protocol === 'responses') return JSON.stringify({ model, input: 'Preview only' }, null, 2)
  return JSON.stringify({ model, ...(protocol === 'messages' ? { max_tokens: 64 } : {}), messages: [{ role: 'user', content: 'Preview only' }] }, null, 2)
}
