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

test('bundleWindowsRuntime copies base Python and merges installed packages', async () => {
  const { bundleWindowsRuntime } = await import('../scripts/hermes-runtime.mjs');
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-hermes-runtime-'));
  const runtimeRoot = path.join(root, 'runtime');
  const sourceRoot = path.join(root, 'managed-python');
  const sourceLink = path.join(root, 'managed-python-link');
  fs.mkdirSync(path.join(sourceRoot, 'Lib'), { recursive: true });
  fs.writeFileSync(path.join(sourceRoot, 'python.exe'), 'python');
  fs.writeFileSync(path.join(sourceRoot, 'Lib', 'os.py'), 'stdlib');
  fs.symlinkSync(sourceRoot, sourceLink, 'dir');
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
