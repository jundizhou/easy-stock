const assert = require('node:assert/strict');
const test = require('node:test');

const {
  createTaogubaBrowserBridge,
  validTaogubaArticleURL,
  validTaogubaURL,
} = require('../taoguba-browser-bridge.cjs');

test('taoguba browser bridge is loopback-only, authenticated, and forwards safe collection input', async (t) => {
  let received;
  const bridge = createTaogubaBrowserBridge({
    collector: async (input) => {
      received = input;
      return { author_name: '测试作者', external_id: '5894557', articles: [], error: '' };
    },
    logger: { warn() {} },
  });
  t.after(() => bridge.close());
  const started = await bridge.start();
  assert.match(started.baseURL, /^http:\/\/127\.0\.0\.1:\d+$/);

  const unauthorized = await fetch(`${started.baseURL}/v1/taoguba/collect`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ profile_id: 'taoguba-default', homepage_url: 'https://www.tgb.cn/blog/5894557' }),
  });
  assert.equal(unauthorized.status, 401);

  const authorized = await fetch(`${started.baseURL}/v1/taoguba/collect`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-A-Stock-Browser-Token': started.token,
    },
    body: JSON.stringify({ profile_id: 'taoguba-default', homepage_url: 'https://www.tgb.cn/blog/5894557', limit: 99 }),
  });
  assert.equal(authorized.status, 200);
  assert.deepEqual(received, {
    profileId: 'taoguba-default',
    homepageURL: 'https://www.tgb.cn/blog/5894557',
    limit: 5,
  });
});

test('taoguba browser bridge rejects other sites and canonicalizes article URLs', () => {
  assert.throws(() => validTaogubaURL('https://example.com/blog/1'), /只允许访问淘股吧/);
  assert.equal(validTaogubaArticleURL('https://www.taoguba.com.cn/a/2u4fmoTIX4i-1?from=feed'), 'https://www.tgb.cn/a/2u4fmoTIX4i');
  assert.equal(validTaogubaArticleURL('https://www.tgb.cn/blog/5894557'), '');
});
