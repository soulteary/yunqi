import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, cpSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';

test('new source files remain visible while runtime data and build outputs stay ignored', t => {
  const root = mkdtempSync(join(tmpdir(), 'yunqi-ignore-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  cpSync(fileURLToPath(new URL('../.gitignore', import.meta.url)), join(root, '.gitignore'));
  mkdirSync(join(root, 'backend-go'));
  cpSync(fileURLToPath(new URL('../backend-go/.gitignore', import.meta.url)), join(root, 'backend-go/.gitignore'));
  const git = args => spawnSync('git', ['-c', 'core.excludesFile=/dev/null', ...args], { cwd: root, encoding: 'utf8' });
  assert.equal(git(['init', '-q']).status, 0);
  const visible = [
    'scripts/new-helper.mjs', 'scripts/new-helper.sh', 'scripts/nested/helper.ts',
    'scripts/test-build.sh', 'frontend/src/test-channel.ts', 'backend-go/cmd/new-tool/main.go',
    'backend-go/cmd/benchmark-report/main.go', 'backend-go/cmd/stream_verify/main.go',
    'backend-go/cmd/ccx-tool/main.go', 'backend-go/cmd/yunqi/main.go',
    '.env.example', '.env.test', 'frontend/bun.lock', 'docs/package-lock.json',
  ];
  const ignored = [
    '.env', 'backend-go/.env', '.env.production.local', '.config/config.json',
    'backend-go/.config/metrics.db', 'logs/request.log', 'scratch/investigation.ts',
    'frontend/node_modules/pkg/index.js', 'frontend/dist/index.html', 'dist/yunqi',
    'backend-go/benchmark-report', 'backend-go/tmp/build',
    'backend-go/stream_verify', 'backend-go/yunqi', 'backend-go/ccx', 'backend-go/ccx-debug',
  ];
  for (const path of visible) {
    assert.equal(git(['check-ignore', '--no-index', '-q', path]).status, 1, `${path} must be visible`);
  }
  for (const path of ignored) {
    assert.equal(git(['check-ignore', '--no-index', '-q', path]).status, 0, `${path} must stay ignored`);
  }
});
