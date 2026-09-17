import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.THEME_UI_URL || 'http://127.0.0.1:20074';
const output = path.resolve(process.env.THEME_UI_OUTPUT || '.runtime/theme-progressive-ui');
await fs.mkdir(output, { recursive: true });
const gate = () => { let release; const promise = new Promise(resolve => { release = resolve; }); return { promise, release }; };
const meta = { source: 'test', fetched_at: '2026-09-17T08:00:00Z', trade_date: '2026-09-17', latency_ms: 1, stale: false };
const stock = (symbol, name) => ({ symbol, name, price: 10, change: 1, change_percent: 2, amount: 100000000, volume: 1000, total_market_cap: 1000000000, float_market_cap: 1000000000, main_net_inflow: 0, rank_score: 80, rank_role: '龙一', meta });
const a1 = stock('000001.SZ', '快速股票');
const a2 = stock('000002.SZ', '慢速股票');
const a3 = stock('000003.SZ', '补充股票');
const b1 = stock('000004.SZ', '芯片股票');
const c1 = stock('000005.SZ', '算力股票');
const themes = [
  { theme: 'kpl:a', name: '通信', leader_stocks: [a1, a2] },
  { theme: 'kpl:b', name: '芯片', leader_stocks: [b1] },
  { theme: 'kpl:c', name: '算力', leader_stocks: [c1] },
];
const kline = symbol => Array.from({ length: 60 }, (_, index) => ({ symbol, time: new Date(Date.UTC(2026, 6, 1 + index)).toISOString(), open: 10 + index / 10, close: 10.2 + index / 10, high: 10.5 + index / 10, low: 9.8 + index / 10, volume: 10000, amount: 1000000, turnover_rate: 2, meta }));

async function fixture(browser, expired = false, permanentlyExpired = false) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  const overview = gate(), full = gate(), slow = gate(), lateB = gate();
  if (expired) { overview.release(); full.release(); slow.release(); lateB.release(); }
  const counts = { recovery: 0, expired: 0, kline: {} };
  await page.routeWebSocket('**/api/v1/ws/stream**', () => {});
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url());
    const send = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) }).catch(() => {});
    if (url.pathname.endsWith('/sources')) return send({ sources: [{ id: 's', name: '快速数据源', category: 'quotes', ok: true, checked_at: meta.fetched_at }] });
    if (url.pathname.endsWith('/market/news')) return send({ data: [{ id: 'n', title: '新闻独立返回，不等待题材', published_at: meta.fetched_at, meta }] });
    if (url.pathname.endsWith('/themes/overview')) {
      await overview.promise;
      if (url.searchParams.get('refresh') === '1') counts.recovery++;
      return send({ data: themes.map((item, index) => ({ ...item, snapshot_id: expired && !counts.recovery ? 'old' : 'new', change_percent: 2, main_net_inflow: 0, rising_nodes: 1, falling_nodes: 0, matched_nodes: 1, total_nodes: 1, top_node_change_percent: 2, daily_strength_score: 90 - index, provisional: true, trade_date: meta.trade_date })), meta, revision: 1, refresh_id: 'fixture', refreshing: false, stage: 'base', steps: {}, errors: {} });
    }
    if (url.pathname.endsWith('/themes/screen')) {
      if (permanentlyExpired || url.searchParams.get('snapshot_id') === 'old') { counts.expired++; return send({ error: '快照已失效', code: 'SNAPSHOT_EXPIRED' }, 410); }
      const id = url.searchParams.get('theme'); const leaders = url.searchParams.get('phase') === 'leaders';
      const theme = themes.find(item => item.theme === id);
      if (!leaders && id === 'kpl:a') await full.promise;
      if (!leaders && id === 'kpl:b') await lateB.promise;
      const stocks = id === 'kpl:a' && !leaders ? [a3, a1, a2] : theme.leader_stocks;
      return send({ data: { complete: !leaders, coverage: leaders ? 'leaders' : 'full', source_snapshot_id: 'new', snapshot_id: 'map-new', order: stocks.map(s => s.symbol), sort: 'rank_score', map: { theme: id, name: theme.name, tabs: [], meta, groups: [{ id: 'members', name: '领涨股', nodes: [{ id: 'members', name: '领涨股', match_status: 'matched', stocks }] }] }, pagination: { page: 1, page_size: 20, total: stocks.length, total_pages: leaders ? 0 : 1, has_more: false } } });
    }
    if (url.pathname.endsWith('/quotes/kline')) {
      const symbol = url.searchParams.get('symbol'); counts.kline[symbol] = (counts.kline[symbol] || 0) + 1;
      if (symbol === a2.symbol && !expired) { await slow.promise; return send({ error: '模拟单股上游失败' }, 502); }
      return send({ data: kline(symbol) });
    }
    return send({ data: [] });
  });
  return { page, context, overview, full, slow, lateB, errors, counts };
}

const browser = await chromium.launch({ headless: true, channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome' });
try {
  const f = await fixture(browser);
  await f.page.goto(`${base}/#themes`);
  await f.page.getByText('新闻独立返回，不等待题材', { exact: true }).waitFor();
  assert.equal(await f.page.locator('.theme-item').count(), 0);
  await f.page.screenshot({ path: path.join(output, 'news-before-overview.png'), fullPage: true });
  f.overview.release();
  await f.page.locator('.stock-table tbody tr').filter({ hasText: '快速股票' }).waitFor();
  await f.page.locator('.candlestick').waitFor();
  assert.match(await f.page.locator('.stock-pagination').innerText(), /完整成分更新中/);
  assert.equal(await f.page.locator('.stock-table tbody tr').filter({ hasText: '补充股票' }).count(), 0);
  await f.page.screenshot({ path: path.join(output, 'chart-before-full-pool.png'), fullPage: true });
  await f.page.locator('.stock-table tbody tr').filter({ hasText: '慢速股票' }).click();
  f.full.release();
  await f.page.locator('.stock-table tbody tr').filter({ hasText: '补充股票' }).waitFor();
  assert.equal(await f.page.locator('.chart-panel h3').innerText(), '慢速股票');
  f.slow.release();
  await f.page.locator('.history-status.partial').waitFor();
  assert.match(await f.page.locator('.chart-panel').innerText(), /日 K 数据暂不可用/);
  assert.equal(f.counts.kline[a1.symbol], 1, 'chart and metrics duplicated first stock request');
  await f.page.screenshot({ path: path.join(output, 'partial-failure.png'), fullPage: true });
  await f.page.locator('.theme-item').filter({ hasText: '芯片' }).click();
  await f.page.locator('.stock-table tbody tr').filter({ hasText: '芯片股票' }).waitFor();
  await f.page.locator('.theme-item').filter({ hasText: '算力' }).click();
  await f.page.locator('.stock-table tbody tr').filter({ hasText: '算力股票' }).waitFor();
  f.lateB.release();
  await f.page.locator('.candlestick').waitFor();
  assert.equal(await f.page.locator('.theme-hero h2').innerText(), '算力');
  assert.equal(await f.page.locator('.chart-panel h3').innerText(), '算力股票');
  assert.equal(await f.page.locator('.stock-table tbody tr').filter({ hasText: '芯片股票' }).count(), 0);
  assert.deepEqual(f.errors, []);
  await f.context.close();

  const recovery = await fixture(browser, true);
  await recovery.page.goto(`${base}/#themes`);
  await recovery.page.locator('.stock-pagination').filter({ hasText: '共 3 只' }).waitFor();
  await recovery.page.locator('.candlestick').waitFor();
  assert.ok(recovery.counts.expired > 0);
  assert.equal(recovery.counts.recovery, 1, 'snapshot recovery must be coalesced');
  assert.deepEqual(recovery.errors, []);
  await recovery.page.screenshot({ path: path.join(output, 'snapshot-recovered.png'), fullPage: true });
  await recovery.context.close();

  const failedRecovery = await fixture(browser, true, true);
  await failedRecovery.page.goto(`${base}/#themes`);
  await failedRecovery.page.getByText('题材快照已更新，请重试', { exact: false }).waitFor();
  assert.match(await failedRecovery.page.locator('.stock-pagination').innerText(), /完整成分暂不可用/);
  assert.equal(failedRecovery.counts.recovery, 1, 'a replacement snapshot failure must stop after one recovery');
  assert.deepEqual(failedRecovery.errors, []);
  await failedRecovery.context.close();

  await fs.writeFile(path.join(output, 'result.json'), JSON.stringify({ passed: true, checks: ['news independence', 'leader rows and chart before full pool', 'per-stock updates', 'partial failure terminates', 'selected stock stays selected', 'stale response ignored', 'one snapshot recovery', 'expired replacement reaches terminal error', 'shared chart/history request'], klineRequests: f.counts.kline }, null, 2));
  console.log('Progressive theme UI checks passed.');
} finally { await browser.close(); }
