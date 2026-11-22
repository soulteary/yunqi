import { createPrivateKey, sign } from 'node:crypto'

// Canonical payload matches presetstore.PresetIndex JSON: fixed field order,
// Go HTML escaping, normalized RFC3339 time, and only enforced shard fields.
export function presetIndexSigningPayload(index) {
  const time = index.publishedAt
    ? new Date(index.publishedAt).toISOString().replace(/\.000Z$/, 'Z').replace(/(\.\d*?[1-9])0+Z$/, '$1Z')
    : '0001-01-01T00:00:00Z'
  const value = {
    signingKeyId: index.signingKeyId,
    schemaVersion: index.schemaVersion,
    dataVersion: index.dataVersion,
    publishedAt: time,
    shards: index.shards.map(shard => ({ kind: shard.kind, url: shard.url, sha256: shard.sha256 })),
  }
  return JSON.stringify(value).replace(/[<>&\u2028\u2029]/g, char => `\\u${char.charCodeAt(0).toString(16).padStart(4, '0')}`)
}
export function signPresetIndex(index, { keyId, privateKey } = {}) {
  if (!keyId || typeof keyId !== 'string' || keyId.length > 64 || !privateKey) throw new Error('Publishing signature requires a key ID and an Ed25519 private key')
  let key
  try { key = createPrivateKey(privateKey) } catch { throw new Error('Invalid Ed25519 publishing key') }
  if (key.asymmetricKeyType !== 'ed25519') throw new Error('Publishing key must be Ed25519')
  const result = { ...index, signingKeyId: keyId }
  if (index.publishedAt) result.publishedAt = new Date(index.publishedAt).toISOString().replace(/\.000Z$/, 'Z').replace(/(\.\d*?[1-9])0+Z$/, '$1Z')
  result.signature = sign(null, Buffer.from(presetIndexSigningPayload(result)), key).toString('base64')
  return result
}
