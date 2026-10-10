// Fixture-only browser checks: no model calls or webhook deliveries.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const output = path.resolve(process.env.PORTFOLIO_UI_OUTPUT || '.runtime/portfolio-schedule-ui');
await fs.mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true, channel: 'chrome' });
const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
await context.addInitScript(() => {
 window.aStock = { getBackendConfig: async () => ({ backendUrl: location.origin, token: '' }) };
 if (!localStorage.getItem('easy-stock.portfolio-plans.v1')) localStorage.setItem('easy-stock.portfolio-plans.v1', JSON.stringify({ activeId: 'a', plans: ['a','b'].map((id) => ({ id, name: id === 'a' ? '成长组合' : '稳健组合', draft: { profile: 'balanced', holdings: [{ symbol: '600519.SH', name: '界面测试股票', weight: 40, costPrice: '100' }] } })) }));
});
const schedules = new Map();
let failSave = false;
let failDelete = false;
let notificationReady = true;
const mutations = [];
await context.route('**/api/**', async (route) => {
 const req = route.request(); const p = new URL(req.url()).pathname;
 let data = [];
 if (p.startsWith('/api/v1/portfolio-schedules/')) {
  const id = p.split('/').at(-1);
  if (req.method() === 'PUT' || req.method() === 'DELETE') {
   mutations.push({ method: req.method(), id });
   if ((req.method() === 'PUT' && failSave) || (req.method() === 'DELETE' && failDelete)) {
    await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: '测试：保存服务暂不可用' }) }); return;
   }
  }
  if (req.method() === 'PUT') schedules.set(id, { ...req.postDataJSON(), next_run_at: '2026-11-01T07:30:00Z', last_run_at: '0001-01-01T00:00:00Z', updated_at: new Date().toISOString() });
  if (req.method() === 'DELETE') schedules.delete(id);
  data = schedules.get(id) || null;
 } else if (p === '/api/v1/settings/notifications') data = { feishu: { enabled: notificationReady, webhook: { configured: notificationReady } }, dingtalk: { enabled: notificationReady, webhook: { configured: notificationReady } }, events: { task_failed: true } };
 else if (p === '/api/v1/stocks/directory') data = { stocks: [] };
 else if (p === '/api/v1/quotes/realtime') data = [{ symbol: '600519.SH', price: 100, trade_time: new Date().toISOString(), meta: { source: 'fixture', stale: false } }];
 else if (p === '/api/v1/agent/status') data = { available: true, configured: true };
 await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data }) });
});
const page = await context.newPage();
page.setDefaultTimeout(10000);
const errors = []; page.on('pageerror', (error) => errors.push(error.message));
const panel = page.locator('.portfolio-schedule');
const pick = (name) => page.getByRole('group', { name: '选择持仓方案' }).getByRole('button', { name: new RegExp(`^${name} `) });
const open = async () => { await panel.locator('summary').click(); await panel.getByRole('button', { name: '保存定时设置' }).waitFor(); };
const save = async () => { await panel.getByRole('button', { name: '保存定时设置' }).click(); await panel.getByRole('status').filter({ hasText: /已保存|已停用/ }).waitFor(); };
try {
 await page.goto(process.env.PORTFOLIO_UI_URL || 'http://127.0.0.1:20123/#portfolio-inspection');
 await open();
 await panel.getByRole('checkbox', { name: '启用「成长组合」定时巡检' }).check();
 await panel.getByRole('button', { name: '每周', exact: true }).click();
 assert.equal(await panel.getByLabel('间隔单位').inputValue(), 'weeks');
 await panel.getByRole('button', { name: '每月', exact: true }).click();
 assert.equal(await panel.getByLabel('间隔单位').inputValue(), 'months');
 await panel.getByRole('button', { name: '每 3 天', exact: true }).click();
 assert.equal(await panel.getByLabel('巡检间隔', { exact: true }).inputValue(), '3');
 await panel.getByLabel('巡检间隔', { exact: true }).fill('2');
 await panel.getByLabel('间隔单位').selectOption('weeks');
 await panel.getByLabel('起始日期').fill('2026-11-01');
 await panel.getByLabel('执行时间（北京时间）').fill('15:30');
 await panel.getByRole('checkbox', { name: '钉钉', exact: true }).check();
 await save();
 assert.deepEqual(schedules.get('a').channels, ['dingtalk']);
 assert.equal(schedules.get('a').interval, 2);
 assert.equal(schedules.get('a').unit, 'weeks');
 assert.equal(schedules.get('a').request.holdings[0].cost_price, 100);
 await page.locator('.portfolio-weight-control input[type=number]').fill('55');
 await panel.getByText(/当前方案已修改/).waitFor();
 assert.equal(schedules.get('a').request.holdings[0].weight_percent, 40, 'editing the draft must not silently change scheduled holdings');
 await save();
 assert.equal(schedules.get('a').request.holdings[0].weight_percent, 55);
 await panel.getByRole('checkbox', { name: '飞书', exact: true }).check();
 await save();
 assert.deepEqual(schedules.get('a').channels, ['dingtalk','feishu']);
 await panel.screenshot({ path: path.join(output, 'desktop.png'), animations: 'disabled' });
 await pick('稳健组合').click();
 await open();
 assert.equal(await panel.getByRole('checkbox', { name: /启用/ }).isChecked(), false);
 await pick('成长组合').click();
 await open();
 assert.equal(await panel.getByLabel('巡检间隔', { exact: true }).inputValue(), '2');
 await page.reload(); await open();
 assert.equal(await panel.getByRole('checkbox', { name: /启用/ }).isChecked(), true);
 await page.setViewportSize({ width: 390, height: 844 });
 await page.waitForFunction(() => document.documentElement.scrollWidth <= innerWidth + 2);
 await panel.screenshot({ path: path.join(output, 'mobile.png'), animations: 'disabled' });
 assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2), false, 'mobile overflow');
 await page.setViewportSize({ width: 1440, height: 1100 });
 failSave = true;
 await panel.getByRole('checkbox', { name: /启用/ }).uncheck();
 await panel.getByRole('button', { name: '保存定时设置' }).click();
 await panel.getByRole('alert').filter({ hasText: '测试：保存服务暂不可用' }).waitFor();
 assert.equal(schedules.get('a').enabled, true);
 failSave = false;
 await save();
 assert.equal(schedules.get('a').enabled, false);
 // Re-enable, then test delete failure preserves the plan and its schedule.
 await panel.getByRole('checkbox', { name: /启用/ }).check(); await save();
 failDelete = true;
 await page.getByRole('button', { name: '删除当前方案', exact: true }).click();
 await page.getByRole('button', { name: '确认删除方案', exact: true }).click();
 await page.locator('.portfolio-error').filter({ hasText: '测试：保存服务暂不可用' }).waitFor();
 assert.equal(schedules.has('a'), true);
 assert.equal(await pick('成长组合').count(), 1);
 failDelete = false;
 await page.getByRole('button', { name: '确认删除方案', exact: true }).click();
 await page.waitForFunction(() => JSON.parse(localStorage.getItem('easy-stock.portfolio-plans.v1')).plans.length === 1);
 assert.equal(schedules.has('a'), false);
 // Missing robots remain unchecked and guide the user to settings.
 notificationReady = false;
 await page.reload(); await open();
 assert.equal(await panel.getByRole('checkbox', { name: /钉钉/ }).isDisabled(), true);
 assert.equal(await panel.getByRole('checkbox', { name: /飞书/ }).isDisabled(), true);
 assert.deepEqual(errors, []);
 await fs.writeFile(path.join(output, 'result.json'), JSON.stringify({ passed: true, fixture_only: true, mutations, errors }, null, 2));
 console.log(`Portfolio schedule UI checks passed: ${output}`);
} catch (error) {
 await page.screenshot({ path: path.join(output, 'failure.png'), fullPage: true });
 await fs.writeFile(path.join(output, 'failure.txt'), `${error}\n${await page.locator('body').innerText()}\n${errors.join('\n')}`);
 throw error;
} finally { await browser.close(); }
