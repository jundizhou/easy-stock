import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, channel: 'chrome' });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
await context.addInitScript(() => { window.aStock = { getBackendConfig: async () => ({ backendUrl: location.origin, token: '' }) }; });
const now = new Date().toISOString();
const holdings = [{ symbol: '600001.SH', name: '界面测试持仓', weight_percent: 80 }];
const request = { portfolio_plan_id: 'default', portfolio_plan_name: '界面测试方案', trader_profile: 'balanced', horizon: 'swing', research_level: 'standard', holdings };
const dimensions = ['holding_logic', 'portfolio_structure', 'risk_capacity', 'strategy_fit'].map((key, i) => ({ key, label: key, score: 70, weight: [35,25,25,15][i], reason: '合成证据', evidence_refs: [], limitations: [] }));
const conclusion = { total_score: 70, score_available: true, risk_level: '中', risk_reason: '合成风险', style_match: '匹配', executive_summary: '合成结论', confidence_level: '中', confidence_reason: '合成质量说明', dimensions, holdings: [], risk_groups: [], primary_risks: [], concentration_findings: [], adjustment_order: [], scenarios: [], next_checklist: [], data_limitations: [] };
const report = { id: 'synthetic-inspection', algorithm_version: 'portfolio-ai-score-v5', prompt_version: 'portfolio-inspection-v10', request, profile: { id: 'balanced', label: '均衡' }, metrics: { total_position_percent: 80, cash_percent: 20, ai_research_coverage_percent: 100, coverage_percent: 100 }, holdings: [], facts: {}, conclusion, generated_at: now };
const inspection = { id: report.id, status: 'succeeded', stage: 'completed', request, report, results: [], started_at: now, updated_at: now };
const plan = { name: '合成优化结果', status: 'conditional', target: holdings, checks: { valid: true, total_position_percent: 80, cash_percent: 20, sold_percent: 0, bought_percent: 0, retained_percent: 80, replacement_ratio_percent: 0, errors: [] }, allocations: [], improvements: [], original_comparison: report, target_comparison: report, assessment: { accepted: true, reason: '合成复评结论', tradeoffs: [], residual_risks: [], evidence_refs: [] } };
const job = { id: 'synthetic-optimization', version: 'portfolio-optimization-v23', model_prompt_version: 'portfolio-optimization-prompts-v36', source_id: report.id, source_report: report, status: 'succeeded', stage: 'completed', outcome: 'conditional', started_at: now, updated_at: now, selected_plan: 0, plans: [plan], results: [], candidates: [], limitations: [], eligibility: [], reused_stocks: 0, new_stocks: 0, model_duration_ms: 0, result_status: { viewable: true, applicable: true, reasons: [] } };
let mode = 'current';
const writes = [], errors = [];
// All APIs are mocked, including writes: never touch real portfolios or models.
await context.route('**/api/**', async route => {
 const req = route.request(), p = new URL(req.url()).pathname;
 if (req.method() !== 'GET') writes.push(p);
 let data = [];
 if (p.startsWith('/api/v1/portfolio-schedules/')) data = null;
 else if (p.endsWith('/optimizations')) data = [job];
 else if (p.includes('/portfolio-optimizations/')) data = { ...job, result_status: mode === 'current' ? job.result_status : mode === 'history' ? { viewable: true, applicable: false, reasons: ['历史方案仍可查看；应用前须按当前规则重新评估'] } : undefined };
 else if (p === '/api/v1/portfolio-inspections') data = [inspection];
 else if (p.startsWith('/api/v1/portfolio-inspections/')) data = inspection;
 else if (p === '/api/v1/agent/status') data = { available: true, configured: true };
 else if (p === '/api/v1/settings/notifications') data = { feishu: { enabled: false, webhook: {} }, dingtalk: { enabled: false, webhook: {} }, events: {} };
 else if (p === '/api/v1/instruments') data = { stocks: [] };
 await route.fulfill({ status: 200, json: { data } });
});
const page = await context.newPage();
page.on('pageerror', e => errors.push(e.stack));
try {
 for (mode of ['current', 'history', 'unknown']) {
  await page.goto(process.env.PORTFOLIO_UI_URL || 'http://127.0.0.1:20073/#portfolio-inspection');
  await page.reload();
  await page.locator('.portfolio-history-select').first().click({timeout:8000});
  const primary = page.locator('.portfolio-optimization-primary');
  await primary.getByRole('heading', { name: '持仓优化结果', exact: true }).waitFor();
  assert.equal(await primary.getByText('本次未生成调整方案', { exact: true }).count(), 0);
  assert.equal(await primary.getByRole('button', { name: '使用该方案发起新巡检' }).count(), mode === 'current' ? 1 : 0);
  if (mode === 'history') assert.match(await primary.innerText(), /历史方案仍可查看/);
 }
 assert.deepEqual(writes, []); assert.deepEqual(errors, []);
 await fs.mkdir('.runtime/portfolio-efficiency', { recursive: true });
 await page.locator('.portfolio-optimization-primary').screenshot({ path: '.runtime/portfolio-efficiency/result-status.png' });
 console.log('PASS: current / historical / missing metadata; no API writes or model calls');
} catch (error) { console.error({ errors, body: await page.locator('body').innerText() }); throw error; } finally { await browser.close(); }
