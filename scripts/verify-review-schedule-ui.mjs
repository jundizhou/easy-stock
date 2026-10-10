// Fixture-only UI validation: no model work, robot delivery, or real settings writes.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, channel: 'chrome' });
const context = await browser.newContext({ viewport: { width: 1440, height: 1050 } });
await context.addInitScript(() => { window.aStock = { getBackendConfig: async () => ({ backendUrl: location.origin, token: '' }) }; });
let saved = { enabled: false, time: '22:00', channels: [], last_window_start: '0001-01-01T00:00:00Z', last_window_end: '0001-01-01T00:00:00Z' };
let failSave = false;
const updates = [];
await context.route('**/api/**', async (route) => {
 const req = route.request(); const path = new URL(req.url()).pathname; let data = [];
 if (path === '/api/v1/reviews/daily-summary/schedule') {
  if (req.method() === 'PUT') {
   if (failSave) { await route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ error: '测试：交易日历获取失败' }) }); return; }
   updates.push(req.postDataJSON()); saved = { ...saved, ...req.postDataJSON(), next_run_at: '2026-10-11T14:00:00Z', target_trade_date: '2026-10-12' };
  }
  data = saved;
 } else if (path === '/api/v1/settings/notifications') {
  const ch = (enabled) => ({ enabled, webhook: { configured: enabled }, secret: { configured: false }, keyword: '' });
  data = { dingtalk: ch(true), feishu: ch(false), events: { stock_research: true, portfolio_inspection: true, task_failed: true } };
 } else if (path === '/api/v1/settings') data = { agent_runtime: 'hermes', llm: { provider: 'openai', model: 'fixture', api_key: { configured: false } }, credentials: {} };
 else if (path === '/api/v1/agent/status') data = { available: true, configured: true };
 else if (path === '/api/v1/stocks/directory') data = { stocks: [] };
 else if (path === '/api/v1/reviews/daily-summary') data = null;
 else if (path.endsWith('/daily-summary/status')) data = { status: 'idle' };
 else if (path.endsWith('/daily-validation')) data = null;
 await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data }) });
});
const page = await context.newPage(); const errors = []; page.on('pageerror', (e) => errors.push(e.message));
page.setDefaultTimeout(10000);
try {
 await page.goto(process.env.REVIEW_UI_URL || 'http://127.0.0.1:20123/#reviews');
 const panel = page.locator('.review-schedule');
 await panel.locator('summary').click();
 await panel.getByRole('button', { name: '保存定时设置' }).waitFor();
 assert.equal(await panel.locator('input[type=time]').inputValue(), '22:00');
 assert.equal(await panel.getByRole('button', { name: '查看最近定时复盘' }).count(), 0);
 assert.equal(await panel.getByRole('checkbox', { name: /飞书/ }).isDisabled(), true);
 await panel.getByRole('checkbox', { name: '启用大V每日定时复盘' }).check();
 await panel.locator('input[type=time]').fill('21:35');
 await panel.getByRole('checkbox', { name: '钉钉', exact: true }).check();
 await panel.getByRole('button', { name: '保存定时设置' }).click();
 await panel.getByRole('status').filter({ hasText: '定时复盘已保存' }).waitFor();
 assert.deepEqual(updates.at(-1), { enabled: true, time: '21:35', channels: ['dingtalk'] });
 await page.reload(); await panel.locator('summary').click();
 await panel.getByRole('button', { name: '保存定时设置' }).waitFor();
 assert.equal(await panel.locator('input[type=time]').inputValue(), '21:35');
 failSave = true;
 await panel.locator('input[type=time]').fill('20:30');
 await panel.getByRole('button', { name: '保存定时设置' }).click();
 await panel.getByRole('alert').filter({ hasText: '交易日历获取失败' }).waitFor();
 assert.equal(saved.time, '21:35');
 failSave = false;
 await panel.getByRole('checkbox', { name: '启用大V每日定时复盘' }).uncheck();
 await panel.getByRole('button', { name: '保存定时设置' }).click();
 await panel.getByRole('status').filter({ hasText: '定时复盘已停用' }).waitFor();
 assert.equal(updates.at(-1).enabled, false);
 await fs.mkdir('.runtime/review-schedule-ui', { recursive: true });
 await page.screenshot({ path: '.runtime/review-schedule-ui/desktop.png', fullPage: true });
 await page.setViewportSize({ width: 760, height: 1000 });
 await page.screenshot({ path: '.runtime/review-schedule-ui/narrow.png', fullPage: true });
 assert.equal(await panel.evaluate((el) => el.scrollWidth <= el.clientWidth + 1), true);
 await panel.getByRole('button', { name: '配置通知机器人' }).click();
 await page.getByText('钉钉与飞书机器人', { exact: true }).waitFor();
 assert.deepEqual(errors, []);
 console.log('Review schedule UI verified: defaults, save/reload, channel validation, failure, disable, layout and settings link.');
} finally { await browser.close(); }
