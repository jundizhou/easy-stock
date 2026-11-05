const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

test('Windows runtime uses a standalone interpreter outside the build venv', async () => {
  const { hermesRuntimePython, hermesVenvPython } = await import('../scripts/hermes-runtime.mjs');
  const root = path.join('C:\\', 'easy-stock', 'hermes-runtime');
  assert.equal(hermesVenvPython(root, 'win32'), path.join(root, 'venv', 'Scripts', 'python.exe'));
  assert.equal(hermesRuntimePython(root, 'win32'), path.join(root, 'python', 'python.exe'));
});

test('bundled Hermes excludes website documents while retaining runtime data', async (t) => {
  const { pruneBundledHermesDocs, robustRemove } = await import('../scripts/hermes-runtime.mjs');
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-hermes-docs-'));
  t.after(() => robustRemove(root));
  for (const name of ['docs/guide.md', 'i18n/zh-Hans/guide.md', 'static/api/model-catalog.json']) {
    const file = path.join(root, 'source', 'website', name);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, name);
  }
  pruneBundledHermesDocs(root);
  assert.equal(fs.existsSync(path.join(root, 'source', 'website', 'docs')), false);
  assert.equal(fs.existsSync(path.join(root, 'source', 'website', 'i18n')), false);
  assert.equal(fs.readFileSync(path.join(root, 'source', 'website', 'static', 'api', 'model-catalog.json'), 'utf8'), 'static/api/model-catalog.json');
});

test('bundleWindowsRuntime copies base Python and merges installed packages', async (t) => {
  const { bundleWindowsRuntime, robustRemove } = await import('../scripts/hermes-runtime.mjs');
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-hermes-runtime-'));
  t.after(() => {
    assert.equal(path.dirname(path.resolve(root)), path.resolve(os.tmpdir()));
    robustRemove(root);
  });
  const runtimeRoot = path.join(root, 'runtime');
  const sourceRoot = path.join(root, 'managed-python');
  const sourceLink = path.join(root, 'managed-python-link');
  fs.mkdirSync(path.join(sourceRoot, 'Lib'), { recursive: true });
  fs.writeFileSync(path.join(sourceRoot, 'python.exe'), 'python');
  fs.writeFileSync(path.join(sourceRoot, 'Lib', 'os.py'), 'stdlib');
  fs.symlinkSync(sourceRoot, sourceLink, process.platform === 'win32' ? 'junction' : 'dir');
  fs.mkdirSync(path.join(runtimeRoot, 'venv', 'Lib', 'site-packages', 'hermes_cli'), { recursive: true });
  fs.writeFileSync(path.join(runtimeRoot, 'venv', 'Lib', 'site-packages', 'hermes_cli', '__init__.py'), 'hermes');
  fs.writeFileSync(path.join(runtimeRoot, 'venv', 'pyvenv.cfg'), `home = ${sourceRoot}\n`);

  bundleWindowsRuntime(runtimeRoot, sourceLink);

  assert.equal(fs.lstatSync(path.join(runtimeRoot, 'python')).isSymbolicLink(), false);
  assert.equal(fs.readFileSync(path.join(runtimeRoot, 'python', 'python.exe'), 'utf8'), 'python');
  assert.equal(fs.readFileSync(path.join(runtimeRoot, 'python', 'Lib', 'os.py'), 'utf8'), 'stdlib');
  assert.equal(fs.readFileSync(path.join(runtimeRoot, 'python', 'Lib', 'site-packages', 'hermes_cli', '__init__.py'), 'utf8'), 'hermes');
  assert.equal(fs.existsSync(path.join(runtimeRoot, 'venv')), false);
});

test('runtime cleanup refuses a filesystem root', async () => {
  const { robustRemove } = await import('../scripts/hermes-runtime.mjs');
  assert.throws(() => robustRemove(path.parse(process.cwd()).root), /Refusing to remove a filesystem root/);
});

test('runtime cleanup removes read-only files without following directory links', async (t) => {
  const { robustRemove } = await import('../scripts/hermes-runtime.mjs');
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-hermes-cleanup-'));
  t.after(() => {
    assert.equal(path.dirname(path.resolve(root)), path.resolve(os.tmpdir()));
    robustRemove(root);
  });
  const target = path.join(root, 'build');
  const external = path.join(root, 'external');
  fs.mkdirSync(target);
  fs.mkdirSync(external);
  const readonly = path.join(target, 'readonly.py');
  const keep = path.join(external, 'keep');
  fs.writeFileSync(readonly, 'temporary');
  fs.writeFileSync(keep, 'keep');
  fs.chmodSync(readonly, 0o444);
  fs.chmodSync(keep, 0o444);
  const originalMode = fs.statSync(keep).mode;
  fs.symlinkSync(external, path.join(target, 'external-link'), process.platform === 'win32' ? 'junction' : 'dir');
  const originalRemove = fs.rmSync;
  let retried = false;
  fs.rmSync = (entry, options) => {
    if (process.platform === 'win32' && path.resolve(entry) === path.resolve(target) && !retried) {
      retried = true;
      throw Object.assign(new Error('read-only build file'), { code: 'EPERM' });
    }
    return originalRemove(entry, options);
  };
  try {
    robustRemove(target);
  } finally {
    fs.rmSync = originalRemove;
  }
  assert.equal(fs.existsSync(target), false);
  assert.equal(fs.readFileSync(keep, 'utf8'), 'keep');
  assert.equal(fs.statSync(keep).mode, originalMode);
  if (process.platform === 'win32') assert.equal(retried, true);
});
