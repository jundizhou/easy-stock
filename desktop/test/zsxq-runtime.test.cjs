const assert = require('node:assert/strict');
const { createHash } = require('node:crypto');
const childProcess = require('node:child_process');
const fs = require('node:fs');
const { syncBuiltinESMExports } = require('node:module');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');
const { resolveZsxqCliPath, zsxqExecutable } = require('../zsxq-runtime.cjs');

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock zsxq 中文 '));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  return root;
}

function file(name, contents = 'native CLI') {
  fs.mkdirSync(path.dirname(name), { recursive: true });
  fs.writeFileSync(name, contents);
  return name;
}

test('CLI release targets pin official native archives with integrity checks', async () => {
  const { zsxqLock, verifyZsxqArchive } = await import('../scripts/zsxq-runtime.mjs');
  for (const target of ['darwin-arm64', 'darwin-x64', 'win32-x64', 'win32-arm64', 'linux-x64', 'linux-arm64']) {
    const asset = zsxqLock.platforms[target];
    assert.equal(asset.package, `@zsxq/cli-${target}`);
    assert.equal(new URL(asset.url).hostname, 'registry.npmjs.org');
    assert.ok(asset.url.endsWith(`-${zsxqLock.version}.tgz`));
    assert.match(asset.integrity, /^sha512-[A-Za-z0-9+/]+=*$/);
  }
  const bytes = Buffer.from('locked download');
  const integrity = 'sha512-' + createHash('sha512').update(bytes).digest('base64');
  verifyZsxqArchive(bytes, integrity);
  assert.throws(() => verifyZsxqArchive(Buffer.from('substituted'), integrity), /integrity mismatch/);
});

for (const platform of ['darwin', 'win32']) {
  test(`${platform} package resolves its native CLI without external Node or npm`, t => {
    const root = fixture(t);
    const bundled = file(zsxqExecutable(path.join(root, 'resources', 'zsxq-runtime'), platform));
    const external = file(path.join(root, 'external', 'zsxq-cli.cmd'));
    const options = { resourcesRoot: path.join(root, 'resources'), isPackaged: true, platform, env: { PATH: '', A_STOCK_ZSXQ_CLI: external }, home: '' };
    assert.equal(resolveZsxqCliPath(options), bundled);
    fs.rmSync(bundled);
    // A damaged package must not silently depend on an unrelated global CLI.
    assert.equal(resolveZsxqCliPath(options), bundled);
  });
}

test('development prefers prepared native CLI and can still resolve explicit or npm installations', t => {
  const root = fixture(t);
  const resourcesRoot = path.join(root, 'resources');
  const explicit = file(path.join(root, 'custom', 'zsxq-cli'));
  const bundled = file(zsxqExecutable(path.join(resourcesRoot, 'zsxq-runtime')));
  const options = { resourcesRoot, env: { A_STOCK_ZSXQ_CLI: explicit }, home: '' };
  assert.equal(resolveZsxqCliPath(options), bundled);
  fs.rmSync(bundled);
  assert.equal(resolveZsxqCliPath(options), explicit);
  const global = file(path.join(root, 'npm bin', 'zsxq-cli.cmd'));
  assert.equal(resolveZsxqCliPath({ resourcesRoot, env: { Path: path.dirname(global) }, platform: 'win32', home: '' }), global);
});

test('corrupt CLI downloads preserve an existing runtime and clean staging files', async t => {
  const { prepareZsxqRuntime } = await import('../scripts/zsxq-runtime.mjs');
  const root = fixture(t);
  const runtimeRoot = path.join(root, 'runtime');
  file(path.join(runtimeRoot, 'keep'), 'existing');
  const archive = file(path.join(root, 'download.tgz'), 'wrong archive');
  await assert.rejects(prepareZsxqRuntime({ runtimeRoot, archive }), /integrity mismatch/);
  assert.equal(fs.readFileSync(path.join(runtimeRoot, 'keep'), 'utf8'), 'existing');
  assert.deepEqual(fs.readdirSync(root).sort(), ['download.tgz', 'runtime']);
});

test('package verification checks native CLI capabilities with a system-only PATH', async t => {
  const { zsxqLock, verifyZsxqRuntime } = await import('../scripts/zsxq-runtime.mjs');
  const root = fixture(t);
  const asset = zsxqLock.platforms[`${process.platform}-${process.arch}`];
  const manifest = { version: zsxqLock.version, package: asset.package, platform: process.platform, arch: process.arch, source: asset.url, integrity: asset.integrity };
  const executable = file(zsxqExecutable(root));
  file(path.join(root, 'LICENSE'));
  file(path.join(root, 'NOTICE'));
  const manifestFile = file(path.join(root, 'runtime-manifest.json'), JSON.stringify(manifest));
  const calls = [];
  let wrongVersion = false;
  let missingCapabilities = false;
  t.mock.method(childProcess, 'spawnSync', (command, args, options) => {
    calls.push(args);
    assert.equal(command, executable);
    const systemPath = process.platform === 'win32' ? path.join(process.env.SystemRoot || 'C:\\Windows', 'System32') : '/usr/bin:/bin';
    assert.equal(options.env.PATH, systemPath);
    assert.equal(Object.keys(options.env).filter(key => key.toUpperCase() === 'PATH').length, 1);
    return { status: 0, stdout: args[0] === '--version' ? `zsxq-cli version ${wrongVersion ? '0.0.0' : zsxqLock.version}\n` : missingCapabilities ? 'login' : '--device-code --no-browser --no-wait' };
  });
  syncBuiltinESMExports();
  t.after(() => { t.mock.restoreAll(); syncBuiltinESMExports(); });
  assert.deepEqual(verifyZsxqRuntime(root), manifest);
  assert.deepEqual(calls, [['--version'], ['auth', 'login', '--help']]);
  wrongVersion = true;
  assert.throws(() => verifyZsxqRuntime(root), /Unexpected .* version/);
  wrongVersion = false;
  missingCapabilities = true;
  assert.throws(() => verifyZsxqRuntime(root), /missing device authorization support/);
  missingCapabilities = false;
  fs.writeFileSync(manifestFile, JSON.stringify({ ...manifest, arch: 'wrong-arch' }));
  assert.throws(() => verifyZsxqRuntime(root), /locked target/);
  fs.writeFileSync(manifestFile, JSON.stringify(manifest));
  fs.rmSync(executable);
  assert.throws(() => verifyZsxqRuntime(root), /ENOENT/);
});

test('development and release preparation both include the CLI runtime', () => {
  const desktopRoot = path.resolve(__dirname, '..');
  const pkg = JSON.parse(fs.readFileSync(path.join(desktopRoot, 'package.json'), 'utf8'));
  assert.match(pkg.scripts.dev, /ensure-zsxq-runtime\.mjs/);
  const prepare = fs.readFileSync(path.join(desktopRoot, 'scripts', 'prepare-package.mjs'), 'utf8');
  assert.match(prepare, /await prepareZsxqRuntime/);
  const verify = fs.readFileSync(path.join(desktopRoot, 'scripts', 'verify-release-package.mjs'), 'utf8');
  assert.match(verify, /verifyZsxqRuntime\(path\.join\(resourcesRoot, 'zsxq-runtime'\)\)/);
});
