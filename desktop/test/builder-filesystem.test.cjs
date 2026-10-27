const assert = require('node:assert/strict');
const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const test = require('node:test');

test('macOS signing scan handles a large source tree under a low descriptor limit', {
  skip: process.platform === 'win32',
}, (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-signing-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  for (let i = 0; i < 1200; i++) {
    fs.writeFileSync(path.join(root, `source-${i}.py`), 'print("Hermes")\n');
  }
  const binary = path.join(root, 'runtime-binary');
  fs.writeFileSync(binary, Buffer.from([0, 1, 2, 3]));
  const setup = pathToFileURL(path.resolve(__dirname, '../scripts/builder-filesystem.mjs')).href;
  const result = spawnSync('/bin/sh', [
    '-c', 'ulimit -n 128; exec "$@"', 'sh', process.execPath, '--input-type=module', '-e',
    `await import(process.argv[1]);
     const { walkAsync } = await import('@electron/osx-sign');
     console.log(JSON.stringify(await walkAsync(process.argv[2])));`,
    setup, root,
  ], { cwd: path.resolve(__dirname, '..'), encoding: 'utf8', timeout: 30_000 });
  assert.equal(result.error, undefined);
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(JSON.parse(result.stdout), [binary]);
});
