// Run with PLAYWRIGHT_MODULE pointing to an installed Playwright package when needed.
// All backend responses are fixtures; this does not use local research data or models.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const server = await createServer({
  root: fileURLToPath(new URL('../frontend', import.meta.url)),
  server: { host: '127.0.0.1', port: 0, open: false },
});
let browser;
try {
  await server.listen();
  const base = server.resolvedUrls.local[0];
  const { etfFixture } = await server.ssrLoadModule('/src/test-utils/etf-fixture.ts');
  const analysis = etfFixture(false);
  analysis.etf.underlying = [{
    holding: { code: '688001', symbol: '688001.SH', name: '财务缺失测试样本', weight_percent: 8.51 },
    holding_report_date: '2026-06-30', financials: null,
    valuation: { pe_ttm: 30, pb: 3, trade_time: '2026-10-19T15:00:00+08:00' },
  }];
  const good = {
    id: analysis.analysis_id, name: '财务缺失测试记录', status: 'succeeded', stage: 'completed',
    request: { symbol: analysis.symbol, purpose: 'observe', horizon: 'swing', analysis_level: 'quantitative' },
    message: '界面测试记录', started_at: analysis.generated_at, updated_at: analysis.generated_at,
    completed_at: analysis.generated_at, analysis,
  };
  const broken = structuredClone(good);
  broken.id = broken.analysis.analysis_id = 'unrenderable-fixture';
  broken.name = '损坏测试记录';
  // A separate malformed field exercises the error boundary after null financials are fixed.
  broken.analysis.etf.profile = null;
  const jobs = [good, broken];

  browser = await chromium.launch({ headless: true, channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome' });
  const context = await browser.newContext({ permissions: ['clipboard-read', 'clipboard-write'] });
  context.setDefaultTimeout(15000);
  await context.addInitScript(({ selectedID }) => {
    if (!localStorage.getItem('easy-stock.stock-research.selected.v1')) {
      localStorage.setItem('easy-stock.stock-research.selected.v1', selectedID);
    }
    window.testRuntimeEvents = [];
    window.aStock = {
      getBackendConfig: async () => ({ backendUrl: location.origin, token: '' }),
      logRuntimeEvent: async (entry) => { window.testRuntimeEvents.push(JSON.parse(entry.message)); },
    };
  }, { selectedID: good.id });
  await context.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname;
    let data;
    if (path === '/api/v1/instruments/research') data = jobs.map(({ analysis: _, ...job }) => job);
    else if (path.startsWith('/api/v1/instruments/research/')) data = jobs.find(job => path.endsWith(`/${job.id}`));
    else if (path === '/api/v1/instruments') data = { stocks: [], warnings: [] };
    else if (path === '/api/v1/stocks/hot-ranks') data = { stocks: [], sources: [] };
    else if (path === '/api/v1/settings' || path === '/api/v1/settings/agent') data = {};
    await route.fulfill({ status: data ? 200 : 503, contentType: 'application/json', body: JSON.stringify(data ? { data } : { error: 'outside fixture scope' }) });
  });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  const sampleRow = () => page.locator('.etf-underlying-table tbody tr');
  const history = name => page.locator('.stock-research-history').getByRole('button', { name: new RegExp(`^${name}`) });
  const assertSample = async () => {
    await sampleRow().waitFor();
    assert.equal(await sampleRow().getByRole('cell', { name: '未取得', exact: true }).count(), 3);
    assert.match(await sampleRow().innerText(), /30\.00 \/ 3\.00/);
  };
  await page.goto(`${base}#stock-ai`, { waitUntil: 'domcontentloaded', timeout: 60000 });
  await assertSample();
  for (const mode of ['情景与核验', '风控执行', '股票 / ETF 分析']) {
    await page.getByRole('button', { name: mode, exact: true }).click();
    await assertSample();
  }
  await page.getByRole('button', { name: '复制报告', exact: true }).click();
  await page.getByRole('button', { name: '已复制', exact: true }).waitFor();
  assert.match(await page.evaluate(() => navigator.clipboard.readText()), /重仓股财务：未取得/);
  await page.reload();
  await assertSample();
  assert.deepEqual(errors, []);

  await history(broken.name).click();
  const failure = page.getByText('分析结果暂时无法显示', { exact: true });
  await failure.waitFor();
  assert.equal(await page.locator('.app-sidebar').isVisible(), true);
  const search = page.getByRole('textbox', { name: '股票或 ETF 名称或代码' });
  await search.fill('510300');
  assert.equal(await search.inputValue(), '510300');
  await page.getByRole('button', { name: '重试显示', exact: true }).click();
  await failure.waitFor();
  assert.ok(await page.evaluate(() => window.testRuntimeEvents.some(event => event.event === 'analysis_render_error')));
  await history(good.name).click();
  await assertSample();
  assert.equal(await failure.count(), 0);
  assert.deepEqual(errors, [], 'render errors must be caught without crashing the app');
  console.log('Passed: restored null financials, all report modes, copy, reload, error fallback, retry, logging, and history recovery.');
} finally {
  await browser?.close();
  await server.close();
}
