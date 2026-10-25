const assert = require('node:assert/strict');
const test = require('node:test');
const { startDesktopWindow, startupPage } = require('../startup.cjs');

test('a visible startup page loads before any background service starts', async () => {
  const steps = [];
  const window = { loadURL: async (url) => { assert.match(decodeURIComponent(url), /正在启动 easy-stock/); steps.push('visible'); }, isDestroyed: () => false };
  assert.equal(await startDesktopWindow({ window, logDirectory: 'C:\\logs', boot: async () => steps.push('boot'), load: async () => steps.push('app'), onError: assert.fail }), true);
  assert.deepEqual(steps, ['visible', 'boot', 'app']);
});

test('backend startup failures remain visible with a log directory action', async () => {
  const pages = [];
  const window = { loadURL: async (url) => pages.push(decodeURIComponent(url)), isDestroyed: () => false };
  const failures = [];
  assert.equal(await startDesktopWindow({ window, logDirectory: 'C:\\用户\\logs', boot: async () => { throw new Error('spawn ENOENT'); }, load: assert.fail, onError: (error) => failures.push(error) }), false);
  assert.equal(failures.length, 1);
  assert.match(pages[1], /easy-stock 启动失败/);
  assert.match(pages[1], /spawn ENOENT/);
  assert.match(pages[1], /打开日志目录/);
  assert.match(pages[1], /C:\\用户\\logs/);
});

test('closed startup windows are not loaded again', async () => {
  let closed = false;
  const window = { loadURL: async () => {}, isDestroyed: () => closed };
  assert.equal(await startDesktopWindow({ window, logDirectory: '/logs', boot: async () => { closed = true; }, load: assert.fail, onError: assert.fail }), false);
});

test('startup errors are escaped and redact secrets', () => {
  const html = decodeURIComponent(startupPage('/logs/<private>', new Error('<script>alert(1)</script> token=private-token')));
  assert.doesNotMatch(html, /<script>alert|private-token|\/logs\/<private>/);
  assert.match(html, /&lt;script&gt;/);
});
