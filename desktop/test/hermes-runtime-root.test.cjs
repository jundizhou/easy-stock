const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const { resolveHermesRuntimeRoot } = require('../hermes-runtime-root.cjs');

function createRuntime(root) {
  const runtimeRoot = path.join(root, 'desktop', 'resources', 'hermes-runtime');
  fs.mkdirSync(path.join(runtimeRoot, 'venv', 'bin'), { recursive: true });
  fs.writeFileSync(path.join(runtimeRoot, 'venv', 'bin', 'python'), 'python');
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
  const runtimeRoot = createRuntime(mainRoot);

  assert.equal(resolveHermesRuntimeRoot({ projectRoot: worktreeRoot, platform: 'darwin' }), runtimeRoot);
});

test('prefers an explicitly configured valid Hermes runtime', () => {
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-explicit-runtime-'));
  const configuredRoot = createRuntime(path.join(tempRoot, 'configured'));
  const bundledRoot = path.join(tempRoot, 'bundle');
  createRuntime(bundledRoot);

  assert.equal(resolveHermesRuntimeRoot({ configuredRoot, bundledRoot: path.join(bundledRoot, 'desktop', 'resources'), platform: 'darwin' }), configuredRoot);
});

test('resolves the Hermes runtime bundled inside a packaged resources directory', () => {
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-packaged-runtime-'));
  const resourcesRoot = path.join(tempRoot, 'easy-stock.app', 'Contents', 'Resources', 'resources');
  const runtimeRoot = path.join(resourcesRoot, 'hermes-runtime');
  fs.mkdirSync(path.join(runtimeRoot, 'venv', 'bin'), { recursive: true });
  fs.writeFileSync(path.join(runtimeRoot, 'venv', 'bin', 'python'), 'python');

  assert.equal(resolveHermesRuntimeRoot({ bundledRoot: resourcesRoot, platform: 'darwin' }), runtimeRoot);
});
