import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const output = path.resolve(process.env.PORTFOLIO_UI_OUTPUT || '.runtime/portfolio-deletion-ui');
await fs.mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true, channel: 'chrome' });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
await context.addInitScript(() => { window.aStock = { getBackendConfig: async () => ({ backendUrl: location.origin, token: '' }) }; });
const fixture = (id, status = 'failed') => ({
	id, status, stage: status, request: { portfolio_plan_id: 'default', portfolio_plan_name: 'UI TEST FIXTURE', trader_profile: 'balanced', holdings: [{ symbol: '600519.SH', name: 'UI TEST FIXTURE', weight_percent: 50 }], horizon: 'swing', research_level: 'standard' },
	results: [], total_stocks: 1, completed_stocks: 0, updated_at: '2026-10-14T07:30:00Z', started_at: '2026-10-14T07:00:00Z',
	resume_available: status === 'failed', error: `UI TEST FIXTURE ${id}`,
});
let jobs = [fixture('selected'), fixture('other')];
let failDelete = true;
let listGate, deleteGate;
let deleteCount = 0;
let listCount = 0;
const writes = [];
// Every API request is intercepted. This test never reads or modifies real records.
await context.route('**/api/**', async (route) => {
	const req = route.request();
	const pathname = new URL(req.url()).pathname;
	if (req.method() !== 'GET') writes.push(`${req.method()} ${pathname}`);
	let data = [];
	if (req.method() === 'DELETE' && pathname.startsWith('/api/v1/portfolio-inspections/')) {
		deleteCount++;
		if (failDelete) return route.fulfill({ status: 409, json: { error: 'UI TEST FIXTURE 删除失败，请重试' } });
		if (deleteGate) await deleteGate;
		if (!jobs.some((item) => item.id === pathname.split('/').at(-1))) return route.fulfill({ status: 404, json: { error: 'record already deleted' } });
		jobs = jobs.filter((item) => item.id !== pathname.split('/').at(-1));
		return route.fulfill({ status: 204 });
	}
	if (pathname === '/api/v1/portfolio-inspections') {
		data = structuredClone(jobs);
		listCount++;
		if (listGate) await listGate;
	} else if (pathname.startsWith('/api/v1/portfolio-inspections/')) data = jobs.find((item) => item.id === pathname.split('/').at(-1));
	else if (pathname.startsWith('/api/v1/portfolio-schedules/')) data = null;
	else if (pathname === '/api/v1/settings/notifications') data = { feishu: { enabled: false, webhook: {} }, dingtalk: { enabled: false, webhook: {} }, events: {} };
	else if (pathname === '/api/v1/instruments') data = { stocks: [] };
	else if (pathname === '/api/v1/agent/status') data = { available: true, configured: true, message: 'UI TEST FIXTURE' };
	await route.fulfill({ status: 200, json: { data } });
});
const page = await context.newPage();
const errors = [];
page.on('pageerror', (error) => errors.push(error.message));
const waitFor = async (predicate, label) => {
	for (let n = 0; n < 100; n++) {
		if (await predicate()) return;
		await new Promise((resolve) => setTimeout(resolve, 50));
	}
	assert.fail(label);
};
try {
	await page.clock.install();
	await page.goto(process.env.PORTFOLIO_UI_URL || 'http://127.0.0.1:20073/#portfolio-inspection');
	const rows = page.locator('.portfolio-history-item');
	await rows.first().waitFor();
	assert.equal(await rows.count(), 2);
	await rows.first().locator('.portfolio-history-select').click();
	await rows.first().locator('.portfolio-history-delete').click();
	await page.getByRole('button', { name: '取消', exact: true }).click();
	assert.equal(deleteCount, 0, 'cancel must not send DELETE');
	await rows.first().locator('.portfolio-history-delete').click();
	await page.getByRole('button', { name: '确认删除', exact: true }).click();
	await page.getByText('UI TEST FIXTURE 删除失败，请重试', { exact: true }).waitFor();
	assert.equal(await rows.count(), 2, 'failure must preserve history');
	assert.equal(await rows.first().locator('.portfolio-history-select').getAttribute('aria-pressed'), 'true');
	let releaseList, releaseDelete;
	listGate = new Promise((resolve) => { releaseList = resolve; });
	const previousLists = listCount;
	await page.clock.runFor(30000);
	await waitFor(() => listCount > previousLists, 'history poll not started');
	failDelete = false;
	deleteGate = new Promise((resolve) => { releaseDelete = resolve; });
	await page.getByRole('button', { name: '确认删除', exact: true }).click();
	await waitFor(() => deleteCount === 2, 'retry DELETE not sent');
	assert.ok(await page.getByRole('button', { name: '正在删除…', exact: true }).isDisabled());
	releaseDelete(); deleteGate = undefined;
	await page.getByText('巡检记录已删除', { exact: true }).waitFor();
	assert.equal(await rows.count(), 1);
	assert.equal(await page.locator('.portfolio-record-binding').count(), 0, 'deleted selection must be cleared');
	const staleResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/portfolio-inspections');
	releaseList(); listGate = undefined;
	await staleResponse;
	await page.clock.runFor(100);
	assert.equal(await rows.count(), 1, 'stale history response resurrected deleted record');
	assert.equal(deleteCount, 2);
	// Verify sibling deletion retains the selected record, then delete the final row.
	jobs.push(fixture('third'));
	await page.reload(); await rows.nth(1).waitFor();
	await rows.first().locator('.portfolio-history-select').click();
	await rows.nth(1).locator('.portfolio-history-delete').click();
	await page.getByRole('button', { name: '确认删除', exact: true }).click();
	await waitFor(async () => await rows.count() === 1, 'sibling not removed');
	assert.equal(await rows.first().locator('.portfolio-history-select').getAttribute('aria-pressed'), 'true');
	await rows.first().locator('.portfolio-history-delete').click();
	jobs = []; // Simulate another window having deleted the final record.
	await page.getByRole('button', { name: '确认删除', exact: true }).click();
	await page.getByText('暂无报告', { exact: true }).waitFor();
	await page.reload(); await page.getByText('暂无报告', { exact: true }).waitFor();
	// Running records and narrow layouts use the same list component.
	jobs = [fixture('completed'), fixture('running', 'running')];
	await page.reload(); await rows.nth(1).waitFor();
	assert.ok(await page.locator('.portfolio-history-delete[title^="巡检进行中"]').isDisabled());
	await page.locator('.portfolio-history-delete:not(:disabled)').first().click();
	await page.screenshot({ path: path.join(output, 'desktop.png'), fullPage: true });
	await page.setViewportSize({ width: 390, height: 844 });
	await page.getByRole('button', { name: '确认删除', exact: true }).scrollIntoViewIfNeeded();
	await page.screenshot({ path: path.join(output, 'mobile.png'), fullPage: true });
	const overflow = await page.locator('.portfolio-history').evaluate((element) => ({ width: element.clientWidth, scroll: element.scrollWidth, right: element.getBoundingClientRect().right, viewport: innerWidth }));
	assert.ok(overflow.scroll <= overflow.width + 2 && overflow.right <= overflow.viewport + 2, `history mobile overflow: ${JSON.stringify(overflow)}`);
	assert.deepEqual(errors, []);
	assert.ok(writes.every((item) => item.startsWith('DELETE /api/v1/portfolio-inspections/')));
	console.log('PASS: confirmation, cancellation, failure/retry, pending disable, selected/sibling/last deletion, stale poll, already-deleted 404, reload, running protection, mobile layout; fixture data only');
} finally {
	await browser.close();
}
