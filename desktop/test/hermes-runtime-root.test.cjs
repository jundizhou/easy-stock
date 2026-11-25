const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const { resolveHermesRuntimeRoot } = require('../hermes-runtime-root.cjs');

function createRuntime(runtimeRoot) {
  for (const file of ['venv/bin/python', 'source/providers/__init__.py', 'source/hermes_cli/__init__.py', 'source/tui_gateway/entry.py']) {
    const target = path.join(runtimeRoot, file);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, '');
  }
  return runtimeRoot;
}

test('resolves Hermes runtime from the main checkout when running inside a git worktree', () => {
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-worktree-runtime-'));
  const mainRoot = path.join(tempRoot, 'main');
  const worktreeRoot = path.join(tempRoot, 'worktrees', 'feature');
  const worktreeGitDir = path.join(mainRoot, '.git', 'worktrees', 'feature');
  fs.mkdirSync(worktreeRoot, { recursive: true });
  fs.mkdirSync(worktreeGitDir, { recursive: true });
  fs.writeFileSync(path.join(worktreeRoot, '.git'), `gitdir: ${worktreeGitDir}\n`);
  fs.writeFileSync(path.join(worktreeGitDir, 'commondir'), '../..\n');
  const runtimeRoot = createRuntime(path.join(mainRoot, 'desktop', 'resources', 'hermes-runtime'));

  assert.equal(resolveHermesRuntimeRoot({ projectRoot: worktreeRoot, platform: 'darwin' }), runtimeRoot);
});

test('prefers an explicitly configured valid Hermes runtime', () => {
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-explicit-runtime-'));
  const configuredRoot = createRuntime(path.join(tempRoot, 'configured'));
  const bundledRoot = path.join(tempRoot, 'bundle');
  createRuntime(path.join(bundledRoot, 'hermes-runtime'));

  assert.equal(resolveHermesRuntimeRoot({ configuredRoot, bundledRoot, platform: 'darwin' }), configuredRoot);
});

test('resolves the Hermes runtime bundled inside a packaged resources directory', () => {
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-packaged-runtime-'));
  const runtimeRoot = createRuntime(path.join(tempRoot, 'easy-stock.app', 'Contents', 'Resources', 'resources', 'hermes-runtime'));

  assert.equal(resolveHermesRuntimeRoot({ bundledRoot: path.dirname(runtimeRoot), platform: 'darwin' }), runtimeRoot);
});

test('skips a partially installed Hermes runtime that still has Python', (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-incomplete-runtime-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const configuredRoot = createRuntime(path.join(root, 'configured'));
  const bundledRoot = createRuntime(path.join(root, 'bundle', 'hermes-runtime'));
  fs.unlinkSync(path.join(configuredRoot, 'source', 'providers', '__init__.py'));

  assert.equal(resolveHermesRuntimeRoot({ configuredRoot, platform: 'darwin' }), '');
  assert.equal(resolveHermesRuntimeRoot({ configuredRoot, bundledRoot: path.dirname(bundledRoot), platform: 'darwin' }), bundledRoot);
});
