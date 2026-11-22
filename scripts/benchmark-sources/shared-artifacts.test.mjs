import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { cpSync, existsSync, mkdtempSync, readFileSync, rmSync, statSync, unlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { renderChannelPresets } from '../generate-channel-presets.mjs'
import { renderSharedArtifacts, synchronizeSharedArtifacts } from '../generate-preset-manifest.mjs'

const repoRoot = dirname(dirname(dirname(fileURLToPath(import.meta.url))))

function fixture(t) {
  const rootDir = mkdtempSync(join(tmpdir(), 'yunqi-shared-artifacts-'))
  t.after(() => rmSync(rootDir, { recursive: true, force: true }))
  cpSync(join(repoRoot, 'shared'), join(rootDir, 'shared'), { recursive: true })
  cpSync(join(repoRoot, 'VERSION'), join(rootDir, 'VERSION'))
  // Publication metadata must survive regeneration on a different day.
  cpSync(join(repoRoot, 'docs/public/presets'), join(rootDir, 'docs/public/presets'), { recursive: true })
  return rootDir
}

function snapshot(artifacts) {
  return artifacts.map(({ path }) => ({
    path,
    content: readFileSync(path, 'utf8'),
    mtimeMs: statSync(path).mtimeMs,
  }))
}

test('shared renderers have no filesystem side effects and generate all consumers', t => {
  const rootDir = fixture(t)
  const frontendPath = join(rootDir, 'frontend/src/generated')
  assert.equal(existsSync(frontendPath), false)
  assert.equal(renderChannelPresets(rootDir).length, 4)
  const artifacts = renderSharedArtifacts(rootDir)
  assert.equal(artifacts.length, 25)
  assert.equal(existsSync(frontendPath), false)
  assert.equal(existsSync(join(rootDir, 'backend-go')), false)

  assert.deepEqual(synchronizeSharedArtifacts({ rootDir }), [])
  const before = snapshot(artifacts)
  assert.deepEqual(synchronizeSharedArtifacts({ rootDir, check: true }), [])
  assert.deepEqual(snapshot(artifacts), before)
  assert.equal(JSON.parse(readFileSync(join(rootDir, 'docs/public/presets/index.json'))).dataVersion,
    JSON.parse(readFileSync(join(repoRoot, 'docs/public/presets/index.json'))).dataVersion)
})

test('read-only check detects every stale or missing generated consumer without repair', t => {
  const rootDir = fixture(t)
  synchronizeSharedArtifacts({ rootDir })
  const artifacts = renderSharedArtifacts(rootDir)
  for (const artifact of artifacts) {
    const relativePath = artifact.path.slice(rootDir.length + 1)
    // Keep valid JSON for the index: its publication metadata is an input to rendering.
    const changed = artifact.path.endsWith('/index.json') ? `${artifact.content}\n` : 'stale\n'
    writeFileSync(artifact.path, changed)
    const before = snapshot(artifacts)
    assert.ok(synchronizeSharedArtifacts({ rootDir, check: true }).includes(relativePath), relativePath)
    assert.deepEqual(snapshot(artifacts), before)
    writeFileSync(artifact.path, artifact.content)
    unlinkSync(artifact.path)
    assert.ok(synchronizeSharedArtifacts({ rootDir, check: true }).includes(relativePath), relativePath)
    assert.equal(existsSync(artifact.path), false)
    writeFileSync(artifact.path, artifact.content)
  }
  assert.deepEqual(synchronizeSharedArtifacts({ rootDir, check: true }), [])
})

test('source edits invalidate frontend, backend, published shards and their checksums', t => {
  const rootDir = fixture(t)
  synchronizeSharedArtifacts({ rootDir })
  const registryPath = join(rootDir, 'shared/model-registry/ccx_model_registry.json')
  const registry = JSON.parse(readFileSync(registryPath))
  registry.regressionMarker = true
  writeFileSync(registryPath, JSON.stringify(registry))
  const channelPath = join(rootDir, 'shared/channel-presets/codex-responses.json')
  const channel = JSON.parse(readFileSync(channelPath))
  const first = Object.values(channel.providers)[0]
  first.noVision = !first.noVision
  writeFileSync(channelPath, JSON.stringify(channel))
  const schemaPath = join(rootDir, 'shared/capability-probe-schema.json')
  const schema = JSON.parse(readFileSync(schemaPath))
  schema.probeModels.chat.push('regression-model')
  writeFileSync(schemaPath, JSON.stringify(schema))
  const differences = synchronizeSharedArtifacts({ rootDir, check: true })
  for (const output of ['docs/public/presets', 'backend-go/internal/presetstore/embedded']) {
    for (const file of ['index.json', 'model-registry.json', 'channel-presets.json']) {
      assert.ok(differences.includes(`${output}/${file}`))
      assert.ok(differences.includes(`${output}/${file}.sha256`))
    }
  }
  assert.ok(differences.includes('frontend/src/generated/codexResponsesPresets.ts'))
  assert.ok(differences.includes('backend-go/internal/handlers/embedded/capability-probe-schema.json'))
  synchronizeSharedArtifacts({ rootDir })
  assert.deepEqual(synchronizeSharedArtifacts({ rootDir, check: true }), [])
})

test('invalid shared schema fails before writing any generated artifact', t => {
  const rootDir = fixture(t)
  synchronizeSharedArtifacts({ rootDir })
  const artifacts = renderSharedArtifacts(rootDir)
  const before = snapshot(artifacts)
  writeFileSync(join(rootDir, 'shared/capability-probe-schema.json'), '{"schemaVersion": 99}')
  assert.throws(() => synchronizeSharedArtifacts({ rootDir }), /schemaVersion must be 1/)
  assert.deepEqual(snapshot(artifacts), before)
})

test('CLI returns nonzero for stale artifacts without modifying or repairing them', t => {
  const rootDir = fixture(t)
  cpSync(join(repoRoot, 'scripts/generate-preset-manifest.mjs'), join(rootDir, 'scripts/generate-preset-manifest.mjs'), { recursive: true })
  cpSync(join(repoRoot, 'scripts/generate-channel-presets.mjs'), join(rootDir, 'scripts/generate-channel-presets.mjs'))
  cpSync(join(repoRoot, 'scripts/preset-signing.mjs'), join(rootDir, 'scripts/preset-signing.mjs'))
  const command = join(rootDir, 'scripts/generate-preset-manifest.mjs')
  assert.equal(spawnSync(process.execPath, [command], { encoding: 'utf8' }).status, 0)
  assert.equal(spawnSync(process.execPath, [command, '--check'], { encoding: 'utf8' }).status, 0)
  const artifact = renderSharedArtifacts(rootDir).find(item => item.path.endsWith('codexResponsesPresets.ts'))
  writeFileSync(artifact.path, 'stale\n')
  const artifacts = renderSharedArtifacts(rootDir)
  const before = snapshot(artifacts)
  const result = spawnSync(process.execPath, [command, '--check'], { encoding: 'utf8' })
  assert.equal(result.status, 1)
  assert.match(result.stderr, /frontend\/src\/generated\/codexResponsesPresets.ts/)
  assert.deepEqual(snapshot(artifacts), before)
  assert.equal(spawnSync(process.execPath, [command, '--unknown'], { encoding: 'utf8' }).status, 1)
})
