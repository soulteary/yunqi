import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createHash, createPrivateKey, createPublicKey, verify } from 'node:crypto'
import { renderPresetManifest } from './generate-preset-manifest.mjs'
import { readFileSync } from 'node:fs'
import { presetIndexSigningPayload, signPresetIndex } from './preset-signing.mjs'

test('publisher signing matches the Go verifier interoperability fixture', () => {
 const fixture=JSON.parse(readFileSync(new URL('../backend-go/internal/presetstore/testdata/signed-price-index.json',import.meta.url),'utf8'))
 // Public test seed only; this key is never trusted by production defaults.
 const key=createPrivateKey({key:Buffer.concat([Buffer.from('302e020100300506032b657004220420','hex'),Buffer.alloc(32,1)]),format:'der',type:'pkcs8'})
 const signed=signPresetIndex(fixture.index,{keyId:'test-release',privateKey:key.export({format:'pem',type:'pkcs8'})})
 assert.equal(signed.signature,fixture.index.signature)
 assert.equal(signed.publishedAt,'2026-11-22T00:00:00.1Z')
 assert.equal(verify(null,Buffer.from(presetIndexSigningPayload(signed)),createPublicKey(key),Buffer.from(signed.signature,'base64')),true)
 assert.equal(presetIndexSigningPayload(signed).includes('sizeBytes'),false)
 assert.equal(presetIndexSigningPayload(signed).includes('00:00:00.1Z'),true)
 assert.equal(presetIndexSigningPayload(signed).includes('\\u0026'),true)
 const changed={...signed,dataVersion:'changed'}
 assert.equal(verify(null,Buffer.from(presetIndexSigningPayload(changed)),createPublicKey(key),Buffer.from(signed.signature,'base64')),false)
})
test('publishing refuses incomplete or non-Ed25519 keys',()=>{
 assert.throws(()=>signPresetIndex({},{}),/requires/)
 assert.throws(()=>signPresetIndex({},{keyId:'release',privateKey:'not-a-key'}),/Invalid Ed25519/)
})

test('the actual artifact publisher signs indexes and refreshes their integrity sidecar',()=>{
 const key=createPrivateKey({key:Buffer.concat([Buffer.from('302e020100300506032b657004220420','hex'),Buffer.alloc(32,1)]),format:'der',type:'pkcs8'})
 const artifacts=renderPresetManifest({metadata:{dataVersion:'test-signed',publishedAt:'2026-11-22T00:00:00Z'},signing:{keyId:'test-release',privateKey:key.export({format:'pem',type:'pkcs8'})}})
 const indexArtifact=artifacts.find(item=>item.path.endsWith('/docs/public/presets/index.json'))
 const index=JSON.parse(indexArtifact.content)
 assert.equal(verify(null,Buffer.from(presetIndexSigningPayload(index)),createPublicKey(key),Buffer.from(index.signature,'base64')),true)
 const sidecar=artifacts.find(item=>item.path===indexArtifact.path+'.sha256')
 assert.equal(sidecar.content.startsWith(createHash('sha256').update(indexArtifact.content).digest('hex')),true)
})
