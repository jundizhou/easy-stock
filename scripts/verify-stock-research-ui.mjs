import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RESEARCH_UI_URL || 'http://127.0.0.1:20074/#stock-ai';
const output = path.resolve(process.env.RESEARCH_UI_OUTPUT || '.runtime/research-ui');
const sample = JSON.parse(await fs.readFile(process.env.RESEARCH_CASE_FILE || '.runtime/research-evaluation/600519.json', 'utf8'));
await fs.mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true, channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome' });
const context = await browser.newContext({ viewport: { width: 1440, height: 1100 }, permissions: ['clipboard-read', 'clipboard-write'], acceptDownloads: true });
const page = await context.newPage();
const errors = [];
page.on('pageerror', (error) => errors.push(error.message));
const analysis = structuredClone(sample.job.analysis);
const claim = { text: '界面测试样例：历史量价不是未来盈利的证明', kind: 'inference', source_ids: ['m-price'] };
const report = {
  headline: '界面测试样例：证据有限，暂不形成交易计划', thesis: claim, support: [claim], counter: [], alternatives: [claim],
  main_conflict: '趋势、经营变化与未来情景仍需分开核实', evidence_level: 'limited', limitations: ['此报告仅用于界面测试，不是真实模型研判'],
  conditions: [{ id: 'c1', text: '核实后续披露是否改变判断', metric: 'disclosure', operator: 'confirmed', window: 'next_disclosure', source_ids: ['f-financial'], status: 'pending' }], invalidation_ids: ['c1'],
  scenarios: [{ key: 'base', name: '基准情景', description: '关键条件尚未改变', condition_ids: ['c1'], response: '等待进一步核实' }],
  decision: { status: 'no_plan', mode: 'non_short', horizon: 'swing', new_position: '暂不形成新仓计划', existing_position: '先核实风险', reason: '本例只验证界面行为', price_plan: null },
  baseline_relation: 'disagree', baseline_reason: '量化历史不证明未来', snapshot_id: sample.snapshot.id, snapshot_version: sample.snapshot.version,
  prompt_version: 'ui-fixture-v1', request: { symbol: analysis.symbol, purpose: 'observe', horizon: 'swing' }, model: 'UI TEST FIXTURE',
  generated_at: sample.snapshot.cutoff_at, cutoff_at: sample.snapshot.cutoff_at, sources: sample.snapshot.sources, anchors: sample.snapshot.anchors,
  questions: [], attempts: [{ stage: 'fixture', duration_ms: 0 }], validation: 'fixture', validation_notes: ['仅为界面测试'],
};
analysis.analysis_id = 'ui-fixture-report';
analysis.research_report = report;
analysis.conclusion = { ...analysis.conclusion, headline: report.headline, summary: claim.text, action: report.decision.new_position, source: 'hermes-research' };
analysis.ai = { status: 'ready', model: report.model, message: '界面测试样例' };
analysis.action_plan = { ...analysis.action_plan, decision_mode: 'non_short', decision_label: '暂不形成交易计划', pricing_source: 'none' };
let job = { ...sample.job, id: analysis.analysis_id, request: report.request, status: 'succeeded', stage: 'completed', message: '界面测试样例', analysis };
let jobs = [job];
let submitted;
let verification = { checked_at: sample.snapshot.cutoff_at, baseline_at: sample.snapshot.cutoff_at, summary: '仅核对条件，不代表预测准确率', checks: [{ condition_id: 'c1', status: 'manual_review', detail: '需要核对原始披露' }] };
await context.route('**/api/v1/stocks/research**', async (route) => {
  const request = route.request();
  const suffix = new URL(request.url()).pathname.split('/stocks/research')[1];
  let data;
  if (!suffix && request.method() === 'POST') {
    submitted = request.postDataJSON();
    job = { id: 'ui-fixture-running', request: submitted, status: 'running', stage: 'researching', message: '界面测试运行中', started_at: new Date().toISOString(), updated_at: new Date().toISOString() };
    jobs = [job, ...jobs]; data = job;
  } else if (!suffix) data = jobs.map((item) => ({ ...item, analysis: undefined, name: item.analysis?.name || item.request.symbol, headline: item.analysis?.conclusion.headline || item.message }));
  else if (suffix.endsWith('/cancel')) { job = { ...job, status: 'cancelled', message: '界面测试已停止' }; jobs = jobs.map((item) => item.id === job.id ? job : item); data = { cancel_requested: true }; }
  else if (suffix.endsWith('/verify')) { job = { ...job, verification }; jobs = jobs.map((item) => item.id === job.id ? job : item); data = verification; }
  else if (request.method() === 'DELETE') { jobs = jobs.filter((item) => item.id !== suffix.slice(1)); data = { deleted: true }; }
  else data = jobs.find((item) => item.id === suffix.slice(1));
  await route.fulfill({ status: data ? 200 : 404, contentType: 'application/json', body: JSON.stringify(data ? { data } : { error: 'fixture not found' }) });
});
const firstHistory = () => page.locator('.stock-research-history article').first().locator('button').first();
const headline = () => page.locator('.stock-research-thesis').getByRole('heading', { name: report.headline, exact: true });
const assertNoOverflow = async (label) => {
	await page.waitForFunction(() => document.documentElement.scrollWidth <= innerWidth + 1, { timeout: 5000 });
  const size = await page.evaluate(() => ({ width: innerWidth, scroll: document.documentElement.scrollWidth }));
  assert.ok(size.scroll <= size.width + 1, `${label}: ${JSON.stringify(size)}`);
};
try {
  await page.goto(base);
  await firstHistory().click(); await headline().waitFor();
  await firstHistory().click(); await headline().waitFor();
  await assertNoOverflow('desktop');
  await page.screenshot({ path: path.join(output, 'desktop.png'), fullPage: true });
  await page.locator('.stock-research-claim button').first().click();
  await page.locator('.stock-research-evidence details[open]').waitFor();
  await assertNoOverflow('evidence');
  await page.screenshot({ path: path.join(output, 'evidence.png'), fullPage: false });
  await page.getByRole('tab', { name: '研判', exact: true }).click();
  await page.getByRole('button', { name: '核对后续行情', exact: true }).click();
  await page.getByText('需人工核实', { exact: true }).waitFor();
  await page.getByRole('button', { name: '复制预案', exact: true }).click();
  assert.ok((await page.evaluate(() => navigator.clipboard.readText())).includes(report.snapshot_id));
  const downloadWait = page.waitForEvent('download');
  await page.getByRole('button', { name: '导出长图', exact: true }).click();
  const download = await downloadWait;
  await download.saveAs(path.join(output, 'export.png'));
  assert.ok((await fs.stat(path.join(output, 'export.png'))).size > 10000);
  await page.getByRole('button', { name: '继续推演', exact: true }).click();
  await page.waitForURL('**/#ai');
  await page.waitForFunction(() => Object.values(localStorage).some((value) => value.includes('"analysis_id":"ui-fixture-report"')));
  await page.locator('.app-sidebar').getByRole('button', { name: '个股分析', exact: true }).click();
  await headline().waitFor(); await page.reload(); await headline().waitFor();
  await page.setViewportSize({ width: 390, height: 844 });
  await assertNoOverflow('mobile');
  await page.screenshot({ path: path.join(output, 'mobile.png'), fullPage: true });
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole('button', { name: '已有持仓', exact: true }).click();
  await page.getByLabel('持仓成本', { exact: true }).fill('123.45');
  await page.getByLabel('研究周期', { exact: true }).selectOption('medium');
  await page.getByLabel('股票名称或代码', { exact: true }).fill('600519');
  await page.getByRole('button', { name: '完整分析', exact: true }).click();
  await page.getByRole('button', { name: '停止本次研究', exact: true }).waitFor();
  assert.equal(submitted.purpose, 'holding'); assert.equal(submitted.horizon, 'medium'); assert.equal(submitted.cost_price, 123.45);
  await page.reload(); await page.getByRole('button', { name: '停止本次研究', exact: true }).click();
  await page.getByText('界面测试已停止', { exact: true }).first().waitFor();
  await page.locator('.stock-research-history article').first().locator('button').last().click();
  await page.waitForFunction(() => !localStorage.getItem('easy-stock.stock-research.selected.v1'));
  assert.deepEqual(errors, []);
  await fs.writeFile(path.join(output, 'result.json'), JSON.stringify({ passed: true, fixture_only: true, workflows: ['history', 'same-id reopen', 'sources', 'verification', 'clipboard', 'image export', 'bound chat', 'navigation/reload', 'mobile', 'holding/cost', 'cancel/reload', 'delete'], errors }, null, 2));
  console.log('12 UI workflows passed using an explicitly labeled fixture; no live-model accuracy claim.');
} finally { await browser.close(); }
