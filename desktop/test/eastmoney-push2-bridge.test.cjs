const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const {
  createEastmoneyPush2Bridge,
  parseProxyTarget,
} = require('../eastmoney-push2-bridge.cjs');

test('eastmoney push2 bridge is authenticated and only forwards push2 quote APIs', async (t) => {
  const seen = [];
  const bridge = createEastmoneyPush2Bridge({
    request: async (input) => {
      seen.push(input);
      return {
        status: 200,
        contentType: 'application/json',
        body: Buffer.from('{"rc":0,"data":{"diff":[]}}'),
      };
    },
    logger: { warn() {} },
  });
  t.after(() => bridge.close());
  const started = await bridge.start();
  assert.match(started.baseURL, /^http:\/\/127\.0\.0\.1:\d+$/);

  const unauthorized = await fetch(`${started.baseURL}/health`);
  assert.equal(unauthorized.status, 401);

  const headers = { 'X-A-Stock-Browser-Token': started.token };
  const target = 'https://90.push2.eastmoney.com/api/qt/stock/fflow/kline/get?secid=90.BK0429&klt=1';
  const proxied = await fetch(`${started.baseURL}/v1/eastmoney/push2?url=${encodeURIComponent(target)}`, { headers });
  assert.equal(proxied.status, 200);
  const payload = await proxied.json();
  assert.equal(payload.ok, true);
  assert.equal(payload.data.status, 200);
  assert.equal(payload.data.content_type, 'application/json');
  assert.equal(payload.data.body, '{"rc":0,"data":{"diff":[]}}');
  assert.ok(payload.data.duration_ms >= 0);
  assert.equal(seen.length, 1);
  assert.equal(seen[0].url, target);
  assert.equal(seen[0].headers.Referer, 'https://quote.eastmoney.com/');
  assert.ok(seen[0].timeoutMs > 0);

  // 非 push2 主机、非 /api/ 路径、伪装的域名都不能借桥转发。
  for (const blocked of [
    'https://example.com/api/qt/stock/fflow/kline/get?x=1',
    'https://push2.eastmoney.com/dataapi/bkzj/getbkzj',
    'https://push2.eastmoney.com.evil.example/api/x',
    'not a url',
  ]) {
    const response = await fetch(`${started.baseURL}/v1/eastmoney/push2?url=${encodeURIComponent(blocked)}`, { headers });
    assert.equal(response.status, 400, blocked);
  }
  assert.equal(seen.length, 1);
});

test('eastmoney push2 bridge reports upstream failures as 502', async (t) => {
  const bridge = createEastmoneyPush2Bridge({
    request: async () => {
      throw new Error('net::ERR_CONNECTION_CLOSED');
    },
    logger: { warn() {} },
  });
  t.after(() => bridge.close());
  const started = await bridge.start();
  const target = 'https://push2.eastmoney.com/api/qt/stock/trends2/get?secid=90.BK0429';
  const response = await fetch(`${started.baseURL}/v1/eastmoney/push2?url=${encodeURIComponent(target)}`, {
    headers: { 'X-A-Stock-Browser-Token': started.token },
  });
  assert.equal(response.status, 502);
  const payload = await response.json();
  assert.equal(payload.ok, false);
  assert.match(payload.error, /ERR_CONNECTION_CLOSED/);
});

test('parseProxyTarget accepts push2 mirrors only', () => {
  assert.equal(
    parseProxyTarget('https://push2.eastmoney.com/api/qt/clist/get?pn=1'),
    'https://push2.eastmoney.com/api/qt/clist/get?pn=1',
  );
  assert.match(
    parseProxyTarget('https://90.push2.eastmoney.com/api/qt/stock/trends2/get?secid=90.BK0429'),
    /90\.push2\.eastmoney\.com/,
  );
  assert.match(
    parseProxyTarget('https://push2his.eastmoney.com/api/qt/stock/kline/get?secid=1.000001'),
    /push2his/,
  );
  assert.equal(parseProxyTarget('https://push2ex.eastmoney.com/getTopicZTPool'), '');
  assert.equal(parseProxyTarget('https://push2.eastmoney.com/api/../secret'), '');
  assert.equal(parseProxyTarget('http://127.0.0.1/api/qt/clist/get'), '');
});

test('main process injects the push2 bridge env the backend reads', () => {
  const main = fs.readFileSync(path.join(__dirname, '..', 'main.cjs'), 'utf8');
  const proxy = fs.readFileSync(
    path.resolve(__dirname, '..', '..', 'backend', 'internal', 'providers', 'eastmoney', 'push2_proxy.go'),
    'utf8',
  );
  const names = [...new Set([...proxy.matchAll(/"(A_STOCK_EASTMONEY_PUSH2_BRIDGE_[A-Z]+)"/g)].map((match) => match[1]))].sort();
  // 环境变量名两端必须一致：拼错只会在打包版里静默退化成“直连被断”。
  assert.deepEqual(names, ['A_STOCK_EASTMONEY_PUSH2_BRIDGE_TOKEN', 'A_STOCK_EASTMONEY_PUSH2_BRIDGE_URL']);
  for (const name of names) {
    assert.ok(main.includes(name), `${name} must be injected by main.cjs`);
  }
  assert.match(main, /bootEastmoneyPush2Bridge\(\)/);
  assert.match(main, /eastmoneyPush2Bridge\?\.close\(\)/);
  const builder = fs.readFileSync(path.join(__dirname, '..', 'scripts', 'electron-builder.mjs'), 'utf8');
  assert.ok(builder.includes("'eastmoney-push2-bridge.cjs'"));
});

test('empty responses back off per endpoint, redact diagnostics and allow recovery', async (t) => {
  let now = 0;
  let recovered = false;
  const calls = [];
  const warnings = [];
  const bridge = createEastmoneyPush2Bridge({
    now: () => now,
    request: async ({ url }) => {
      calls.push(url);
      if (recovered || url.includes('/trends2/')) return { status: 200, body: '{}' };
      if (now === 0) throw new Error('net::ERR_EMPTY_RESPONSE at ' + url);
      return { status: 200, body: Buffer.from('  ') };
    },
    logger: { warn: warning => warnings.push(warning) },
  });
  t.after(() => bridge.close());
  const { baseURL, token } = await bridge.start();
  const get = async (path) => {
    const target = new URL(path, 'https://push2.eastmoney.com');
    target.searchParams.set('ut', 'sensitive-value');
    return fetch(baseURL + '/v1/eastmoney/push2?url=' + encodeURIComponent(target.href), {
      headers: { 'X-A-Stock-Browser-Token': token },
    });
  };
  const quote = '/api/qt/stock/get';
  assert.equal((await get(quote)).status, 502);
  const burst = await Promise.all(Array.from({ length: 8 }, () => get(quote)));
  assert.ok(burst.every(response => response.status === 503));
  assert.equal((await get('/api/qt/stock/trends2/get')).status, 200);
  assert.equal(calls.length, 2);
  now += 10_000;
  assert.equal((await get(quote)).status, 502);
  assert.equal((await get(quote)).status, 503);
  assert.equal(warnings.length, 2);
  assert.match(warnings[0], /stage=request .*code=ERR_EMPTY_RESPONSE/);
  assert.match(warnings[1], /stage=response .*code=EMPTY_BODY/);
  assert.doesNotMatch(warnings.join(' '), /sensitive-value|ut=|https:/);
  now += 20_000;
  recovered = true;
  assert.equal((await get(quote)).status, 200);
  assert.equal((await get(quote)).status, 200);
});
