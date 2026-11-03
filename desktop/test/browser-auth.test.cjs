const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const {
  LEGACY_TAOGUBA_AUTH_MARKER,
  TAOGUBA_AUTH_MARKER,
  hasLoggedInTaogubaSession,
  hasLoggedInXueqiuSession,
  partitionForTaogubaProfile,
  partitionForXueqiuProfile,
  playwrightCookie,
  readBrowserAuthStatus,
  statePathForProfile,
  writeStorageState,
} = require('../browser-auth.cjs');

test('xueqiu profiles use stable isolated partitions and state paths', () => {
  assert.equal(partitionForXueqiuProfile('xueqiu-default'), partitionForXueqiuProfile('xueqiu-default'));
  assert.notEqual(partitionForXueqiuProfile('xueqiu-default'), partitionForXueqiuProfile('xueqiu-second'));
  assert.match(partitionForXueqiuProfile('xueqiu-default'), /^persist:a-stock-ai-xueqiu-/);
  const statePath = statePathForProfile('/tmp/auth', 'xueqiu-default');
  assert.equal(path.dirname(statePath), path.join('/tmp/auth', 'xueqiu'));
  assert.match(path.basename(statePath), /^[a-f0-9]{32}\.json$/);
});

test('taoguba profiles use a separate persistent partition and source state directory', () => {
  assert.notEqual(partitionForTaogubaProfile('shared-profile'), partitionForXueqiuProfile('shared-profile'));
  const statePath = statePathForProfile('/tmp/auth', 'taoguba-default', 'taoguba');
  assert.equal(path.dirname(statePath), path.join('/tmp/auth', 'taoguba'));
  assert.match(path.basename(statePath), /^[a-f0-9]{32}\.json$/);
});

test('electron cookies are exported as Playwright storage state without exposing them in status', (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'a-stock-browser-auth-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const filePath = statePathForProfile(root, 'xueqiu-default');
  const storageState = {
    cookies: [playwrightCookie({
      name: 'xq_is_login',
      value: '1',
      domain: '.xueqiu.com',
      path: '/',
      secure: true,
      httpOnly: true,
      sameSite: 'lax',
    })],
    origins: [],
  };
  writeStorageState(filePath, storageState);

  assert.equal(hasLoggedInXueqiuSession(storageState), true);
  assert.deepEqual(JSON.parse(fs.readFileSync(filePath, 'utf8')), storageState);
  // Windows reports DOS read/write attributes here, not its actual ACL. Keep
  // the POSIX permission guarantee on platforms where these bits represent it.
  if (process.platform !== 'win32') assert.equal(fs.statSync(filePath).mode & 0o777, 0o600);
  const status = readBrowserAuthStatus(filePath);
  assert.equal(status.configured, true);
  assert.doesNotMatch(JSON.stringify(status), /xq_is_login|cookie/i);
});

test('taoguba login status is persisted from verified page state without exposing account data', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'a-stock-taoguba-auth-'));
  const filePath = statePathForProfile(root, 'taoguba-default', 'taoguba');
  const storageState = {
    cookies: [playwrightCookie({ name: 'session', value: 'secret', domain: '.tgb.cn', path: '/', secure: true })],
    origins: [{ origin: 'https://www.tgb.cn', localStorage: [{ name: TAOGUBA_AUTH_MARKER, value: '1' }] }],
  };
  writeStorageState(filePath, storageState);

  assert.equal(hasLoggedInTaogubaSession(storageState), true);
  const status = readBrowserAuthStatus(filePath, 'taoguba');
  assert.equal(status.configured, true);
  assert.match(status.message, /淘股吧登录态已保存在本机/);
  assert.doesNotMatch(JSON.stringify(status), /secret|session|verified/i);
});

test('legacy taoguba login marker remains valid after the product rename', () => {
  const storageState = {
    cookies: [],
    origins: [{ origin: 'https://www.tgb.cn', localStorage: [{ name: LEGACY_TAOGUBA_AUTH_MARKER, value: '1' }] }],
  };
  assert.equal(hasLoggedInTaogubaSession(storageState), true);
});
