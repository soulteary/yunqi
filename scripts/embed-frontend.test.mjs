import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, cpSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';

const sourceRoot = fileURLToPath(new URL('../', import.meta.url));

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'yunqi-embed-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const dir of ['scripts', 'frontend/src', 'frontend/public', 'shared', 'backend-go', 'bin']) {
    mkdirSync(join(root, dir), { recursive: true });
  }
  for (const file of ['Makefile', 'backend-go/Makefile', 'scripts/embed-frontend.sh']) {
    cpSync(join(sourceRoot, file), join(root, file));
  }
  for (const file of ['frontend/src/main.ts', 'frontend/package.json', 'frontend/bun.lock', 'shared/schema.json']) {
    writeFileSync(join(root, file), '{}');
  }
  // Stub both tools: recipes execute, but no service or real compiler is started.
  writeFileSync(join(root, 'bin/bun'), '#!/bin/sh\necho build >> "$FIXTURE_ROOT/builds"\nmkdir -p dist/assets\necho index > dist/index.html\necho asset > dist/assets/app.js\n', { mode: 0o755 });
  writeFileSync(join(root, 'bin/go'), '#!/bin/sh\ntest -f frontend/dist/index.html || exit 12\necho "$1" >> "$FIXTURE_ROOT/go-calls"\n', { mode: 0o755 });
  const run = (args = ['embed-frontend'], cwd = root, overrides = {}) => {
    const result = spawnSync('make', args, { cwd, encoding: 'utf8', env: { ...process.env, PATH: `${join(root, 'bin')}:${process.env.PATH}`, FIXTURE_ROOT: root, ...overrides } });
    assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
  };
  const builds = () => readFileSync(join(root, 'builds'), 'utf8').trim().split('\n').length;
  return { root, run, builds };
}

test('content cache invalidates dependency/shared inputs, deletion and incomplete embed', t => {
  const { root, run, builds } = fixture(t);
  run();
  run();
  assert.equal(builds(), 1);
  for (const file of ['frontend/package.json', 'frontend/bun.lock', 'shared/schema.json', 'frontend/src/main.ts']) {
    writeFileSync(join(root, file), 'changed');
    run();
  }
  assert.equal(builds(), 5);
  rmSync(join(root, 'shared/schema.json'));
  run();
  assert.equal(builds(), 6);
  rmSync(join(root, 'backend-go/frontend/dist/index.html'));
  run();
  assert.equal(builds(), 7);
});

test('root and backend entrypoints share validation without destructive recopy', t => {
  const { root, run, builds } = fixture(t);
  run(['build']);
  run(['run']);
  for (const target of ['build', 'build-local', 'run', 'copy-frontend']) {
    run([target], join(root, 'backend-go'));
  }
  assert.equal(builds(), 1);
  assert.deepEqual(readFileSync(join(root, 'go-calls'), 'utf8').trim().split('\n'), ['build', 'run', 'build', 'build', 'run']);
});


test('Vite language environment and production dotenv files invalidate the build', t => {
  const { root, run, builds } = fixture(t);
  run(['embed-frontend'], root, { APP_UI_LANGUAGE: 'en' });
  run(['embed-frontend'], root, { APP_UI_LANGUAGE: 'en' });
  assert.equal(builds(), 1);
  run(['embed-frontend'], root, { APP_UI_LANGUAGE: 'zh-CN' });
  assert.equal(builds(), 2);
  for (const file of ['.env', '.env.local', '.env.production', '.env.production.local']) {
    writeFileSync(join(root, 'frontend', file), 'APP_UI_LANGUAGE=zh-CN');
    run(['embed-frontend'], root, { APP_UI_LANGUAGE: 'zh-CN' });
  }
  assert.equal(builds(), 6);
});
