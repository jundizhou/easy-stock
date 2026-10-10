import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const output = path.resolve('.runtime/portfolio-binding-ui');
await fs.mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true, channel: 'chrome' });
const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
const now = new Date().toISOString();
const holdings = [{ symbol: '600001.SH', name: '保留股票', weight_percent: 50, cost_price: 8 }, { symbol: '600002.SH', name: '卖出股票', weight_percent: 30, cost_price: 10 }];
const target = [{ ...holdings[0], weight_percent: 30 }, { symbol: '600003.SH', name: '新增股票', weight_percent: 50 }];
const plans = { activeId: 'plan-a', plans: [
 { id: 'plan-a', name: '长线方案', draft: { profile: 'steady', totalAssets: 800000, horizon: 'medium', researchLevel: 'deep', holdings: holdings.map((h) => ({ symbol: h.symbol, name: h.name, weight: h.weight_percent, costPrice: String(h.cost_price) })) } },
 { id: 'plan-b', name: '波段方案', draft: { profile: 'balanced', totalAssets: 500000, horizon: 'swing', researchLevel: 'standard', holdings: [{ symbol: '600004.SH', name: '其他方案股票', weight: 20, costPrice: '30' }] } },
] };
await context.addInitScript((seed) => {
 window.aStock = { getBackendConfig: async () => ({ backendUrl: location.origin, token: '' }) };
 if (!localStorage.getItem('easy-stock.portfolio-plans.v1')) localStorage.setItem('easy-stock.portfolio-plans.v1', JSON.stringify(seed));
}, plans);
const req = { portfolio_plan_id: 'plan-a', portfolio_plan_name: '长线方案', trader_profile: 'steady', horizon: 'medium', research_level: 'deep', holdings };
const dimensions = (score) => ['holding_logic', 'portfolio_structure', 'risk_capacity', 'strategy_fit'].map((key, i) => ({ key, label: key, score, weight: [35, 25, 25, 15][i], reason: '界面测试', evidence_refs: [] }));
const conclusion = (score) => ({ score_available: true, total_score: score, risk_level: '中', executive_summary: '方案绑定测试', dimensions: dimensions(score), holdings: [] });
const report = { id: 'source-a', algorithm_version: 'portfolio-ai-score-v4', request: req, profile: { label: '稳重' }, holdings: holdings.map((holding) => ({ holding, status: 'succeeded', research_origin: 'reused' })), metrics: { total_position_percent: 80, cash_percent: 20 }, facts: {}, conclusion: conclusion(60), generated_at: now };
const source = { id: report.id, request: req, report, results: report.holdings, status: 'succeeded', updated_at: now };
const other = { ...structuredClone(source), id: 'source-b', request: { ...req, portfolio_plan_id: 'plan-b', portfolio_plan_name: '波段方案' } };
other.report.id = other.id; other.report.request = other.request;
const legacy = { ...structuredClone(source), id: 'legacy', request: { ...req, portfolio_plan_id: undefined, portfolio_plan_name: undefined } };
legacy.report.id = legacy.id; legacy.report.request = legacy.request;
let records = [source, other, legacy];
const plan = { name: '建议配置', status: 'accepted', target, checks: { valid: true, total_position_percent: 80, cash_percent: 20, retained_percent: 30, replacement_ratio_percent: 62.5 }, allocations: [], improvements: [], original_comparison: report, target_comparison: { ...report, request: { ...req, holdings: target }, conclusion: conclusion(75) }, assessment: { accepted: true, reason: '改善组合', tradeoffs: [], residual_risks: [], evidence_refs: [] } };
const optimization = { id: 'optimization-a', source_id: source.id, root_source_id: source.id, version: 'portfolio-optimization-v23', model_prompt_version: 'portfolio-optimization-prompts-v34', source_report: report, baseline: holdings, plans: [plan], selected_plan: 0, status: 'succeeded', outcome: 'accepted', stage: 'completed', results: [], candidates: [], eligibility: [], limitations: [], started_at: now, updated_at: now };
let quoteMode = 'missing';
const prices = { '600001.SH': 12.5, '600002.SH': 10, '600003.SH': 25, '600004.SH': 30 };
const historyQueries = [];
let submitted;
await context.route('**/api/**', async (route) => {
 const request = route.request(); const url = new URL(request.url()); const p = url.pathname;
 let data = [];
 if (p === '/api/v1/stocks/directory') data = { stocks: [...holdings, target[1]].map((h) => ({ symbol: h.symbol, name: h.name, code: h.symbol.split('.')[0] })) };
 else if (p === '/api/v1/quotes/realtime') data = url.searchParams.get('symbols').split(',').filter((symbol) => quoteMode !== 'missing' || symbol !== '600003.SH').map((symbol) => ({ symbol, price: prices[symbol], trade_time: now, meta: { source: 'fixture', fetched_at: now, stale: quoteMode === 'stale' && symbol === '600003.SH' } }));
 else if (p === '/api/v1/portfolio-inspections' && request.method() === 'GET') {
  historyQueries.push(url.searchParams.has('portfolio_plan_id') ? url.searchParams.get('portfolio_plan_id') : 'all');
  data = records.filter((record) => !url.searchParams.has('portfolio_plan_id') || (record.request.portfolio_plan_id || '') === url.searchParams.get('portfolio_plan_id'));
 } else if (p === '/api/v1/portfolio-inspections' && request.method() === 'POST') {
  submitted = request.postDataJSON(); data = { ...source, id: 'new-inspection', request: submitted, report: undefined, status: 'running', stage: 'aggregating', started_at: now, total_stocks: 2, completed_stocks: 2, current_symbols: [] }; records.unshift(data);
 } else if (p.endsWith('/bind-plan')) {
  const record = records.find((item) => item.id === p.split('/').at(-2));
  record.request = { ...record.request, ...request.postDataJSON() }; record.report.request = record.request; data = record;
 } else if (p.endsWith('/optimizations')) data = [optimization];
 else if (p.startsWith('/api/v1/portfolio-optimizations/')) data = optimization;
 else if (p.startsWith('/api/v1/portfolio-inspections/')) data = records.find((item) => item.id === p.split('/').at(-1));
 else if (p === '/api/v1/agent/status') data = { available: true, configured: true };
 await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data }) });
});
const page = await context.newPage();
page.setDefaultTimeout(15000);
const errors = []; page.on('pageerror', (e) => errors.push(e.message));
const saved = () => page.evaluate(() => JSON.parse(localStorage.getItem('easy-stock.portfolio-plans.v1')));
const pick = (name) => page.getByRole('group', { name: '选择持仓方案' }).getByRole('button', { name: new RegExp(`^${name} `) });
const history = page.locator('.portfolio-history > div > button');
const apply = page.getByRole('button', { name: '应用到原方案', exact: true });
try {
 await page.goto(process.env.PORTFOLIO_UI_URL || 'http://127.0.0.1:20123/#portfolio-inspection', { timeout: 60000 });
 await history.first().waitFor(); assert.equal(await history.count(), 1); assert.equal(historyQueries.at(-1), 'plan-a');
 await pick('波段方案').click();
 await page.waitForFunction(() => document.querySelector('.portfolio-history header small')?.textContent === '波段方案');
 await history.first().click(); await page.getByText('所属方案：波段方案', { exact: true }).waitFor();
 assert.equal(historyQueries.at(-1), 'plan-b');
 await page.getByLabel('巡检记录范围').selectOption('all');
 await history.filter({ hasText: '长线方案' }).click();
 await page.getByText('所属方案：长线方案', { exact: true }).waitFor(); await apply.waitFor();
 assert.equal(await pick('波段方案').getAttribute('aria-pressed'), 'true');
 const before = await saved(); const beforeReport = JSON.stringify(source.report);
 await apply.click(); await page.getByRole('alert').filter({ hasText: '原方案未修改' }).waitFor(); assert.deepEqual(await saved(), before);
 quoteMode = 'stale'; await apply.click(); await page.getByRole('alert').filter({ hasText: '原方案未修改' }).waitFor(); assert.deepEqual(await saved(), before);
 quoteMode = 'valid'; await apply.click(); await page.getByText('已应用到「长线方案」，全部持仓成本已更新为现价', { exact: true }).waitFor();
 const after = await saved(); assert.equal(after.activeId, 'plan-b'); assert.deepEqual(after.plans[1], before.plans[1]);
 assert.equal(after.plans[0].draft.totalAssets, 800000); assert.equal(after.plans[0].draft.profile, 'steady');
 assert.deepEqual(after.plans[0].draft.holdings.map((h) => [h.symbol, h.weight, h.costPrice, h.entryPrice]), [['600001.SH', 30, '12.5', 12.5], ['600003.SH', 50, '25', 25]]);
 assert.equal(JSON.stringify(source.report), beforeReport); assert.equal(await page.locator('.portfolio-comparison-side').first().getByText('原成本 8', { exact: true }).count(), 1);
 await page.screenshot({ path: path.join(output, 'applied.png'), fullPage: true });
 await page.setViewportSize({ width: 390, height: 844 });
 await page.waitForFunction(() => document.documentElement.scrollWidth <= innerWidth + 2, undefined, { timeout: 3000 });
 await page.screenshot({ path: path.join(output, 'mobile.png'), fullPage: true });
 await page.setViewportSize({ width: 1440, height: 1100 });
 await page.getByRole('button', { name: '查看持仓方案' }).click();
 await page.locator('.portfolio-cost-input input').first().waitFor();
 assert.equal(await pick('长线方案').getAttribute('aria-pressed'), 'true'); assert.equal(await page.locator('.portfolio-cost-input input').first().inputValue(), '12.5');
 await page.reload(); await history.first().click(); await apply.waitFor();
 assert.equal((await saved()).plans[0].draft.holdings[1].costPrice, '25');
 // A deleted plan remains traceable in history but cannot overwrite the active one.
 await page.getByRole('button', { name: '删除当前方案', exact: true }).click();
 await page.getByRole('button', { name: '确认删除方案', exact: true }).click();
 await page.getByLabel('巡检记录范围').selectOption('all');
 await history.filter({ hasText: '长线方案' }).click();
 await page.getByText('所属方案：长线方案（已删除）', { exact: true }).waitFor(); await apply.waitFor(); assert.equal(await apply.isDisabled(), true);
 // Explicitly bind a legacy report; even its old optimization snapshot uses the new source binding.
 await page.getByLabel('巡检记录范围').selectOption('unbound'); await history.first().click();
 await page.getByRole('button', { name: '绑定到「波段方案」' }).click(); await page.getByText('所属方案：波段方案', { exact: true }).waitFor();
 await apply.waitFor(); assert.equal(await apply.isEnabled(), true); await apply.click();
 await page.getByText('已应用到「波段方案」，全部持仓成本已更新为现价', { exact: true }).waitFor();
 assert.equal((await saved()).plans[0].draft.totalAssets, 500000);
 await page.getByRole('button', { name: '使用该方案发起新巡检' }).click(); await page.getByRole('button', { name: '停止持仓分析' }).waitFor();
 assert.equal(submitted.portfolio_plan_id, 'plan-b'); assert.equal(submitted.portfolio_plan_name, '波段方案'); assert.equal(submitted.source_optimization_id, 'optimization-a');
 assert.deepEqual(errors, []);
 await fs.writeFile(path.join(output, 'result.json'), JSON.stringify({ passed: true, fixture_only: true, checks: ['scoped history', 'cross-plan application by stable ID', 'missing/stale quotes retain original', 'all costs replaced with current prices', 'asset/settings/report preservation', 'persistent holdings', 'deleted plan disabled', 'legacy binding', 'optimization-derived inspection binding', 'mobile overflow'], errors }, null, 2));
 console.log(`Portfolio binding UI checks passed: ${output}`);
} catch (error) {
 await page.screenshot({ path: path.join(output, 'failure.png'), fullPage: true });
 await fs.writeFile(path.join(output, 'failure.txt'), `${error}\n${JSON.stringify(errors)}\n${await page.locator('body').innerText()}`); throw error;
} finally { await browser.close(); }
