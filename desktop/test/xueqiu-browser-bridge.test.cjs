const assert = require('node:assert/strict');
const test = require('node:test');

const {
  createXueqiuBrowserBridge,
  validXueqiuArticleURL,
  validXueqiuURL,
} = require('../xueqiu-browser-bridge.cjs');

test('xueqiu browser bridge is loopback-only, authenticated, and forwards safe collection input', async (t) => {
  let received;
  const bridge = createXueqiuBrowserBridge({
    collector: async (input) => {
      received = input;
      return { author_name: '测试作者', external_id: '123', articles: [], error: '' };
    },
    logger: { warn() {} },
  });
  t.after(() => bridge.close());
  const started = await bridge.start();
  assert.match(started.baseURL, /^http:\/\/127\.0\.0\.1:\d+$/);

  const unauthorized = await fetch(`${started.baseURL}/v1/xueqiu/collect`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ profile_id: 'xueqiu-default', homepage_url: 'https://xueqiu.com/u/123' }),
  });
  assert.equal(unauthorized.status, 401);

  const authorized = await fetch(`${started.baseURL}/v1/xueqiu/collect`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-A-Stock-Browser-Token': started.token,
    },
    body: JSON.stringify({ profile_id: 'xueqiu-default', homepage_url: 'https://xueqiu.com/u/123', limit: 99 }),
  });
  assert.equal(authorized.status, 200);
  assert.deepEqual(received, {
    profileId: 'xueqiu-default',
    homepageURL: 'https://xueqiu.com/u/123',
    limit: 5,
  });
});

test('xueqiu browser bridge rejects non-xueqiu URLs and canonicalizes article URLs', () => {
  assert.throws(() => validXueqiuURL('https://example.com/u/123'), /只允许访问雪球/);
  assert.equal(validXueqiuArticleURL('https://xueqiu.com/123/456?from=feed'), 'https://xueqiu.com/123/456');
  assert.equal(validXueqiuArticleURL('https://xueqiu.com/u/123'), '');
});
