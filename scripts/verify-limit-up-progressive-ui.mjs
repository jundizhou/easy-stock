import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.LIMIT_UP_UI_URL || 'http://127.0.0.1:20074';
const output = path.resolve(process.env.LIMIT_UP_UI_OUTPUT || '.runtime/limit-up-progressive-ui');
await fs.mkdir(output, { recursive: true });
const gate = () => { let release; const promise = new Promise(resolve => { release = resolve; }); return { promise, release }; };
const meta = { source: 'test', fetched_at: '2026-09-17T06:00:00Z', trade_date: '2026-09-17', stale: false };
const stock = { symbol: '600001.SH', name: '快速梯队股票', price: 10, change_percent: 10, amount: 100000000, float_market_cap: 1000000000, turnover_rate: 5, streak: 3, open_count: 0, days: 3, count: 3, is_st: false, limit_regime: '10cm', source: 'kaipanla', raw_concepts: ['算力'] };
const day = (date, streak) => ({ trade_date: date, limit_up_count: 1, board_count: 1, first_board_count: 0, max_streak: streak, reopened_count: 0, st_count: 0, total_amount: stock.amount, levels: [{ level: streak, label: `${streak}板`, count: 1, stocks: [{ ...stock, streak }] }] });
const ladder = { current: day('2026-09-17', 3), previous: day('2026-09-16', 2), comparison_ready: false, session_status: '盘中快照', advance: [], concept_heat: [], industry_heat: [], concept_status: 'loading', concept_meta: meta, meta };
const raw = { final_break_rate: .2, previous_limit_up_return: 1.2, previous_board_return: 2, advance_rate: .4, theme_focus: .5, high_risk_score: 30 };
const history = { points: [15, 16].map(date => ({ trade_date: `2026-09-${date}`, emotion_score: 60 + date % 3, phase: '发酵/主升', raw })), cache: { cached_days: 2, last_external_sync: '2026-09-16' } };
const intraday = { trade_date: '2026-09-17', session_status: '盘中快照', updated_at: meta.fetched_at, stale: false, confidence: '盘中试算', risk_score: 20, status: '高位延续', breadth: '低位活跃', metrics: { high_levels: [2], high_average_return: 4, high_down_rate: .2, height_collapse: 0, high_advance_rate: .5 } };
const progress = (data, refreshing = false, steps = {}, errors = {}, revision = 1) => ({ data, refreshing, steps, errors, revision, refresh_id: 'fixture', stale: false });

async function fixture(browser, scenario) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  const page = await context.newPage(); page.setDefaultTimeout(30000);
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  const firstLadder = gate(), historyGate = gate(), enrichment = gate(), staleResponse = gate();
  let complete = false, ladderRequests = 0, entryRequests = 0;
  const counters = { history: 0, ladder: 0 };
  await page.routeWebSocket('**/api/v1/ws/stream**', () => {});
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url());
    const send = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) }).catch(() => {});
    if (url.pathname.endsWith('/short-term/emotion-history')) {
      counters.history++;
      if (scenario === 'slow-history') await historyGate.promise;
      return send(progress(history));
    }
    if (url.pathname.endsWith('/short-term/limit-up-ladder')) {
      counters.ladder++; ladderRequests++;
      assert.equal(url.searchParams.get('delivery'), 'progressive');
      if (!url.searchParams.has('refresh_id')) entryRequests++;
      if (scenario === 'history-first') await firstLadder.promise;
      if (scenario === 'stale-response' && entryRequests === 1) {
        await staleResponse.promise;
        return send(progress({ ...ladder, current: day('2026-09-15', 1) }));
      }
      if (scenario === 'stale-response') return send(progress(ladder));
      if (url.searchParams.get('refresh') === '1') return send(progress(null, false, { primary: 'error' }, { primary: '模拟刷新失败' }, 4));
      if (ladderRequests === 1) return send(progress(ladder, true, { quotes: 'loading', themes: 'loading', history: 'loading' }));
      if (!complete) { await enrichment.promise; complete = true; }
      return send(progress({ ...ladder, comparison_ready: true, advance: [{ from_level: 2, to_level: 3, base: 1, success: 1, rate: 1 }], intraday }, false, { quotes: 'ready', concepts: 'error' }, { concepts: '模拟概念目录失败' }, 3));
    }
    if (url.pathname.endsWith('/themes/overview')) return send({ data: [], meta, refreshing: false, steps: {}, errors: {} });
    if (url.pathname.endsWith('/sources')) return send({ sources: [] });
    return send({ data: [] });
  });
  return { page, context, errors, counters, firstLadder, historyGate, enrichment, staleResponse };
}

const browser = await chromium.launch({ headless: true, channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome' });
try {
  const f = await fixture(browser, 'history-first');
  await f.page.goto(`${base}/#limit-up`, { waitUntil: 'domcontentloaded', timeout: 60000 });
  await f.page.locator('.emotion-line-chart').waitFor();
  await f.page.locator('.current-ladder-panel .limit-up-loading').waitFor();
  assert.deepEqual(await f.page.locator('.limit-summary-card > strong').allTextContents(), ['—', '—', '—', '—', '—']);
  await f.page.screenshot({ path: path.join(output, 'history-before-ladder.png'), fullPage: true });
  f.firstLadder.release();
  await f.page.getByText(stock.name, { exact: true }).waitFor();
  await f.page.locator('.advance-panel').getByText('正在补充昨日梯队，晋级率稍后显示。').waitFor();
  assert.match(await f.page.locator('.emotion-intraday-card').innerText(), /更新中/);
  await f.page.screenshot({ path: path.join(output, 'ladder-before-enrichment.png'), fullPage: true });
  f.enrichment.release();
  await f.page.locator('.emotion-intraday-card').getByText('高位延续', { exact: true }).waitFor();
  await f.page.getByRole('status').filter({ hasText: '模拟概念目录失败' }).waitFor();
  await f.page.getByRole('button', { name: '刷新梯队', exact: true }).click();
  await f.page.getByRole('status').filter({ hasText: '模拟刷新失败' }).waitFor();
  assert.equal(await f.page.getByText(stock.name, { exact: true }).count(), 1, 'failed refresh removed existing rows');
  assert.equal(await f.page.locator('.emotion-line-chart').count(), 1);
  assert.deepEqual(f.errors, []);
  await f.page.screenshot({ path: path.join(output, 'refresh-failure-retains-data.png'), fullPage: true });
  await f.context.close();

  const slow = await fixture(browser, 'slow-history');
  await slow.page.goto(`${base}/#limit-up`, { waitUntil: 'domcontentloaded', timeout: 60000 });
  await slow.page.getByText(stock.name, { exact: true }).waitFor();
  assert.equal(await slow.page.locator('.emotion-line-chart').count(), 0);
  slow.enrichment.release();
  await slow.page.locator('.emotion-intraday-card').getByText('高位延续', { exact: true }).waitFor();
  assert.equal(await slow.page.locator('.emotion-line-chart').count(), 0, 'intraday unexpectedly waited for historical bootstrap');
  slow.historyGate.release();
  await slow.page.locator('.emotion-line-chart').waitFor();
  assert.deepEqual(slow.errors, []);
  await slow.context.close();

  const stale = await fixture(browser, 'stale-response');
  await stale.page.goto(`${base}/#limit-up`, { waitUntil: 'domcontentloaded', timeout: 60000 });
  await stale.page.locator('.emotion-line-chart').waitFor();
  await stale.page.getByRole('button', { name: '趋势题材', exact: true }).click();
  await stale.page.getByRole('button', { name: '短线连板', exact: true }).click();
  await stale.page.locator('.limit-up-hero').getByText('2026-09-17', { exact: false }).waitFor();
  stale.staleResponse.release();
  await stale.page.waitForTimeout(200);
  assert.match(await stale.page.locator('.limit-up-hero').innerText(), /2026-09-17/);
  assert.doesNotMatch(await stale.page.locator('.limit-up-hero').innerText(), /2026-09-15/);
  assert.deepEqual(stale.errors, []);
  await stale.context.close();
  await fs.writeFile(path.join(output, 'result.json'), JSON.stringify({ passed: true, checks: ['history before ladder', 'unknown metrics are placeholders', 'base ladder before enrichment', 'intraday before historical bootstrap', 'enrichment failure is local', 'refresh failure retains data', 'old response ignored after reentry'] }, null, 2));
  console.log('Progressive limit-up UI checks passed.');
} finally { await browser.close(); }
