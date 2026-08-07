const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const {
  WECHAT_RUNTIME_MANIFEST,
  buildWechatEnv,
  resolveWechatPython,
  statusFromLoginInfo,
  syncWechatServiceSource,
} = require('../wechat-service.cjs');

test('buildWechatEnv configures a local-only sidecar', () => {
  const env = buildWechatEnv({ baseURL: 'http://127.0.0.1:23001', port: 23001, baseEnv: { PATH: '/bin', NO_PROXY: 'example.com' } });
  assert.equal(env.PATH, '/bin');
  assert.equal(env.HOST, '127.0.0.1');
  assert.equal(env.PORT, '23001');
  assert.equal(env.SITE_URL, 'http://127.0.0.1:23001');
  assert.equal(env.ENABLE_MCP, '0');
  assert.equal(env.SKIP_BACKGROUND_TASKS, '1');
  assert.match(env.NO_PROXY, /127\.0\.0\.1/);
});

test('resolveWechatPython follows the bundled Hermes runtime layout', () => {
  assert.equal(resolveWechatPython('/runtime', 'darwin'), path.join('/runtime', 'venv', 'bin', 'python'));
  assert.equal(resolveWechatPython('C:\\runtime', 'win32'), path.join('C:\\runtime', 'venv', 'Scripts', 'python.exe'));
});

test('syncWechatServiceSource upgrades code without replacing login credentials', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'a-stock-wechat-test-'));
  const sourceDir = path.join(root, 'source');
  const workDir = path.join(root, 'work');
  fs.mkdirSync(path.join(sourceDir, 'routes'), { recursive: true });
  fs.writeFileSync(path.join(sourceDir, 'app.py'), 'version = 2\n');
  fs.writeFileSync(path.join(sourceDir, 'routes', 'login.py'), 'login = True\n');
  fs.writeFileSync(path.join(sourceDir, WECHAT_RUNTIME_MANIFEST), JSON.stringify({ revision: 'rev-2' }));
  fs.mkdirSync(path.join(workDir, 'data'), { recursive: true });
  fs.writeFileSync(path.join(workDir, 'data', '.credentials.json'), '{"token":"keep-me"}');
  fs.writeFileSync(path.join(workDir, '.env'), 'WECHAT_TOKEN=keep-me\n');
  fs.writeFileSync(path.join(workDir, 'obsolete.py'), 'remove me\n');
  fs.writeFileSync(path.join(workDir, WECHAT_RUNTIME_MANIFEST), JSON.stringify({ revision: 'rev-1' }));

  const result = syncWechatServiceSource({ sourceDir, workDir });

  assert.equal(result.updated, true);
  assert.equal(fs.readFileSync(path.join(workDir, 'app.py'), 'utf8'), 'version = 2\n');
  assert.equal(fs.readFileSync(path.join(workDir, 'data', '.credentials.json'), 'utf8'), '{"token":"keep-me"}');
  assert.equal(fs.readFileSync(path.join(workDir, '.env'), 'utf8'), 'WECHAT_TOKEN=keep-me\n');
  assert.equal(fs.existsSync(path.join(workDir, 'obsolete.py')), false);
});

test('statusFromLoginInfo reports login and expiration states', () => {
  const now = Date.parse('2026-08-07T10:00:00Z');
  const ready = statusFromLoginInfo({ success: true, data: { nickname: '测试号', fakeid: 'fake-id', expire_time: now + 60_000 } }, now, 'http://127.0.0.1:23001');
  assert.equal(ready.authenticated, true);
  assert.equal(ready.account, '测试号');
  assert.equal(ready.login_url, 'http://127.0.0.1:23001/login.html');
  assert.match(ready.message, /自动历史文章列表暂不可用/);

  const expired = statusFromLoginInfo({ success: true, data: { nickname: '测试号', expire_time: now - 1 } }, now);
  assert.equal(expired.authenticated, false);
  assert.equal(expired.state, 'expired');

  const missing = statusFromLoginInfo({ success: false }, now, 'http://127.0.0.1:23001');
  assert.equal(missing.available, true);
  assert.equal(missing.state, 'not_logged_in');
  assert.match(missing.message, /已知微信公众号文章链接/);
});
