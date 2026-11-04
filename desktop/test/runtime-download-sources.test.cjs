const assert = require('node:assert/strict');
const childProcess = require('node:child_process');
const fs = require('node:fs');
const { syncBuiltinESMExports } = require('node:module');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

function fixtureRoot(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-download-source-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  return root;
}

function environment(t, key, value) {
  const previous = process.env[key];
  if (value === undefined) delete process.env[key];
  else process.env[key] = value;
  t.after(() => {
    if (previous === undefined) delete process.env[key];
    else process.env[key] = previous;
  });
}

function mockCommands(t, handler) {
  t.mock.method(childProcess, 'spawnSync', handler);
  syncBuiltinESMExports();
  t.after(() => {
    t.mock.restoreAll();
    syncBuiltinESMExports();
  });
}

for (const proxy of [false, true]) {
  test(`Hermes ${proxy ? 'proxy' : 'default'} downloads retain pinned archive verification`, async (t) => {
    // Import first: the selected transport must be read when preparation runs.
    const { HERMES_AGENT_SOURCE, prepareHermesRuntime } = await import('../scripts/hermes-runtime.mjs');
    const mirror = 'https://mirror.example.invalid/hermes.tar.gz';
    environment(t, 'HERMES_AGENT_SOURCE', proxy ? ` ${mirror} ` : ' ');
    environment(t, 'HERMES_AGENT_ARCHIVE', undefined);
    const root = fixtureRoot(t);
    const runtimeRoot = path.join(root, 'runtime');
    fs.mkdirSync(runtimeRoot);
    fs.writeFileSync(path.join(runtimeRoot, 'keep'), 'existing runtime');
    const downloads = [];
    mockCommands(t, (command, args) => {
      assert.equal(command, 'curl', 'unverified archives must not be extracted or executed');
      const output = args.indexOf('-o');
      downloads.push(args[output - 1]);
      fs.writeFileSync(args[output + 1], 'a substituted archive from the selected download source');
      return { status: 0 };
    });

    assert.throws(() => prepareHermesRuntime({ runtimeRoot, sourcePath: '' }), /checksum mismatch/);
    assert.deepEqual(downloads, [proxy ? mirror : HERMES_AGENT_SOURCE]);
    assert.match(HERMES_AGENT_SOURCE, /^https:\/\/codeload\.github\.com\/NousResearch\/hermes-agent\//);
    assert.equal(fs.readFileSync(path.join(runtimeRoot, 'keep'), 'utf8'), 'existing runtime');
    assert.deepEqual(fs.readdirSync(root), ['runtime']);
  });

  test(`WeChat ${proxy ? 'proxy' : 'default'} downloads preserve canonical source notices`, async (t) => {
    const { WECHAT_DOWNLOAD_API_REPOSITORY, WECHAT_DOWNLOAD_API_REVISION, prepareWechatRuntime } = await import('../scripts/wechat-runtime.mjs');
    const mirror = 'https://mirror.example.invalid/tmwgsicp/wechat-download-api';
    environment(t, 'WECHAT_DOWNLOAD_API_REPOSITORY', proxy ? ` ${mirror}/// ` : undefined);
    const root = fixtureRoot(t);
    const runtimeRoot = path.join(root, 'runtime');
    const resourcesRoot = path.join(root, 'resources');
    fs.mkdirSync(path.join(runtimeRoot, 'venv', 'lib', 'python3.14', 'site-packages'), { recursive: true });
    fs.mkdirSync(path.join(runtimeRoot, 'python', 'bin'), { recursive: true });
    fs.writeFileSync(path.join(runtimeRoot, 'python', 'bin', 'python3.14'), 'fixture interpreter');
    const downloads = [];
    mockCommands(t, (command, args, options) => {
      if (command === 'curl') {
        const output = args.indexOf('-o');
        downloads.push(args[output - 1]);
        fs.writeFileSync(args[output + 1], 'fixture archive');
      } else if (command === 'tar') {
        const source = path.join(options.cwd, 'wechat-download-api-fixture');
        fs.mkdirSync(source);
        fs.writeFileSync(path.join(source, 'app.py'), '# source fixture');
        fs.writeFileSync(path.join(source, 'LICENSE'), 'fixture license');
      }
      return { status: 0 };
    });

    const manifest = prepareWechatRuntime({ resourcesRoot, runtimeRoot, sourcePath: '' });
    const expectedRepository = proxy ? mirror : WECHAT_DOWNLOAD_API_REPOSITORY;
    assert.deepEqual(downloads, [`${expectedRepository}/archive/${WECHAT_DOWNLOAD_API_REVISION}.tar.gz`]);
    assert.equal(manifest.repository, 'https://github.com/tmwgsicp/wechat-download-api');
    const targetRoot = path.join(resourcesRoot, 'wechat-download-api');
    assert.equal(JSON.parse(fs.readFileSync(path.join(targetRoot, 'easy-stock-wechat-runtime.json'), 'utf8')).repository, manifest.repository);
    assert.ok(fs.readFileSync(path.join(targetRoot, 'EASY_STOCK_SOURCE_NOTICE.md'), 'utf8').includes(manifest.repository));
    assert.equal(fs.readFileSync(path.join(targetRoot, 'app.py'), 'utf8'), '# source fixture');
  });
}
