const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const {
  acquireEastmoneySession,
  createEastmoneySessionBridge,
  isEastmoneyCookie,
  readEastmoneySessionCookie,
} = require('../eastmoney-session-bridge.cjs');

test('eastmoney session bridge is loopback-only, authenticated, and hands the session cookie to callers', async (t) => {
  const calls = [];
  const bridge = createEastmoneySessionBridge({
    acquire: async (input) => {
      calls.push(input);
      return { name: 'nid18', value: 'abc123', expires_at: '2099-01-01T00:00:00.000Z' };
    },
    logger: { warn() {} },
  });
  t.after(() => bridge.close());
  const started = await bridge.start();
  assert.match(started.baseURL, /^http:\/\/127\.0\.0\.1:\d+$/);

  const unauthorized = await fetch(`${started.baseURL}/v1/eastmoney/session`);
  assert.equal(unauthorized.status, 401);

  const authorized = await fetch(`${started.baseURL}/v1/eastmoney/session`, {
    headers: { 'X-A-Stock-Browser-Token': started.token },
  });
  assert.equal(authorized.status, 200);
  const payload = await authorized.json();
  assert.deepEqual(payload, {
    ok: true,
    data: { cookie: 'nid18=abc123', expires_at: '2099-01-01T00:00:00.000Z' },
  });
  assert.deepEqual(calls, [{ refresh: false, timeoutMs: 20_000 }]);

  // 会话尚未过期时复用缓存，不再打扰浏览器。
  const cached = await fetch(`${started.baseURL}/v1/eastmoney/session`, {
    headers: { 'X-A-Stock-Browser-Token': started.token },
  });
  assert.equal((await cached.json()).data.cookie, 'nid18=abc123');
  assert.equal(calls.length, 1);

  // refresh=1 跳过缓存，强制重新取一份会话。
  const refreshed = await fetch(`${started.baseURL}/v1/eastmoney/session?refresh=1`, {
    headers: { 'X-A-Stock-Browser-Token': started.token },
  });
  assert.equal(refreshed.status, 200);
  assert.equal(calls.length, 2);
  assert.equal(calls[1].refresh, true);
});

test('eastmoney session bridge keeps the previous session when a refresh fails', async (t) => {
  let fail = false;
  const bridge = createEastmoneySessionBridge({
    acquire: async () => {
      if (fail) throw new Error('窗口加载失败');
      return { name: 'nid18', value: 'first', expires_at: '' };
    },
    logger: { warn() {} },
  });
  t.after(() => bridge.close());
  const started = await bridge.start();
  const headers = { 'X-A-Stock-Browser-Token': started.token };
  assert.equal((await (await fetch(`${started.baseURL}/v1/eastmoney/session`, { headers })).json()).data.cookie, 'nid18=first');

  fail = true;
  const degraded = await fetch(`${started.baseURL}/v1/eastmoney/session?refresh=1`, { headers });
  assert.equal(degraded.status, 200);
  assert.equal((await degraded.json()).data.cookie, 'nid18=first');
});

test('eastmoney session bridge reports acquisition failures', async (t) => {
  const bridge = createEastmoneySessionBridge({
    acquire: async () => { throw new Error('等待东方财富会话 Cookie（nid18）超时'); },
    logger: { warn() {} },
  });
  t.after(() => bridge.close());
  const started = await bridge.start();
  const response = await fetch(`${started.baseURL}/v1/eastmoney/session`, {
    headers: { 'X-A-Stock-Browser-Token': started.token },
  });
  assert.equal(response.status, 502);
  assert.equal((await response.json()).ok, false);
});

test('main process injects the session bridge env the backend reads', () => {
  const main = fs.readFileSync(path.join(__dirname, '..', 'main.cjs'), 'utf8');
  const session = fs.readFileSync(
    path.resolve(__dirname, '..', '..', 'backend', 'internal', 'providers', 'eastmoney', 'session.go'),
    'utf8',
  );
  const names = [...new Set([...session.matchAll(/"(A_STOCK_EASTMONEY_SESSION_BRIDGE_[A-Z]+)"/g)].map((match) => match[1]))].sort();
  // 环境变量名两端必须一致：拼错只会在打包版里静默退化成“没有会话”。
  assert.deepEqual(names, ['A_STOCK_EASTMONEY_SESSION_BRIDGE_TOKEN', 'A_STOCK_EASTMONEY_SESSION_BRIDGE_URL']);
  for (const name of names) {
    assert.ok(main.includes(name), `${name} must be injected by main.cjs`);
  }
  assert.match(main, /bootEastmoneySessionBridge\(\)/);
  assert.match(main, /eastmoneySessionBridge\?\.close\(\)/);
});

test('acquireEastmoneySession waits for the page load when refreshing', async () => {
  const events = [];
  let stored = null;
  const BrowserWindow = function fakeWindow() {
    return {
      webContents: {
        session: { cookies: { get: async () => (stored ? [stored] : []) } },
        setWindowOpenHandler() {},
      },
      loadURL: async (url) => {
        events.push(`load:${url}`);
        // 页面加载结束前上游会话并没有恢复，此时 Cookie 还取不到。
        await new Promise((resolve) => setTimeout(resolve, 5));
        stored = { domain: '.eastmoney.com', name: 'nid18', value: 'from-page' };
        return {};
      },
      isDestroyed: () => false,
      destroy() {
        events.push('destroy');
      },
    };
  };

  const acquired = await acquireEastmoneySession({ BrowserWindow, refresh: true, timeoutMs: 5_000 });
  assert.deepEqual(acquired, { name: 'nid18', value: 'from-page', expires_at: '' });
  // 顺序必须是「先加载页面、再读 Cookie」，否则拿不到刷新后的会话。
  assert.deepEqual(events, ['load:https://data.eastmoney.com/bkzj/gn.html', 'destroy']);
});

test('acquireEastmoneySession reuses the stored cookie without loading the page', async () => {
  const events = [];
  const BrowserWindow = function fakeWindow() {
    return {
      webContents: {
        session: {
          cookies: {
            get: async () => [{ domain: '.eastmoney.com', name: 'nid18', value: 'stored' }],
          },
        },
        setWindowOpenHandler() {},
      },
      loadURL: async (url) => {
        events.push(`load:${url}`);
        return {};
      },
      isDestroyed: () => false,
      destroy() {
        events.push('destroy');
      },
    };
  };

  const acquired = await acquireEastmoneySession({ BrowserWindow, refresh: false, timeoutMs: 5_000 });
  assert.equal(acquired.value, 'stored');
  // 命中已有会话时不必加载页面（窗口仍会关闭）。
  assert.deepEqual(events.filter((event) => event.startsWith('load:')), []);
});

test('readEastmoneySessionCookie only accepts live eastmoney.com cookies', () => {
  const now = Date.UTC(2026, 9, 18);
  assert.equal(readEastmoneySessionCookie([{ domain: '.sina.com.cn', name: 'nid18', value: 'x' }], now), null);
  assert.equal(readEastmoneySessionCookie([{ domain: '.eastmoney.com', name: 'nid18', value: '' }], now), null);
  assert.equal(
    readEastmoneySessionCookie([{ domain: '.eastmoney.com', name: 'nid18', value: 'x', expirationDate: now / 1000 - 60 }], now),
    null,
  );
  assert.deepEqual(
    readEastmoneySessionCookie([{ domain: '.eastmoney.com', name: 'nid18', value: ' live ', expirationDate: now / 1000 + 3600 }], now),
    { name: 'nid18', value: 'live', expires_at: new Date(now + 3_600_000).toISOString() },
  );
  assert.deepEqual(
    readEastmoneySessionCookie([{ domain: 'data.eastmoney.com', name: 'nid18', value: 'session' }], now),
    { name: 'nid18', value: 'session', expires_at: '' },
  );
  assert.equal(isEastmoneyCookie({ domain: '.eastmoney.com' }), true);
  assert.equal(isEastmoneyCookie({ domain: 'eastmoney.com.cn' }), false);
});
