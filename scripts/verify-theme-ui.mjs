import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.THEME_UI_URL || 'http://127.0.0.1:20074';
const output = path.resolve(process.env.THEME_UI_OUTPUT || '.runtime/theme-ui');
await fs.mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true, channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome' });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
context.setDefaultTimeout(20000);
const page = await context.newPage();
const errors = [];
page.on('pageerror', error => errors.push(error.message));
const key = 'easy-stock.theme.v1';
const toggle = () => page.locator('.theme-toggle');
const checkTheme = async theme => {
  await page.waitForFunction(value => document.documentElement.dataset.theme === value, theme);
  assert.equal(await toggle().getAttribute('aria-pressed'), String(theme === 'dark'));
  assert.equal(await page.evaluate(() => getComputedStyle(document.documentElement).colorScheme), theme);
};
const checkButtonFits = async () => {
  const box = await toggle().boundingBox();
  assert.ok(box && box.x >= 0 && box.x + box.width <= page.viewportSize().width, JSON.stringify(box));
};
const screenshot = name => page.screenshot({ path: path.join(output, `${name}.png`), fullPage: true, animations: 'disabled' });

try {
  await page.goto(`${base}/#portfolio-inspection`);
  await toggle().waitFor();
  await checkTheme('light');
  await screenshot('portfolio-light');
  await toggle().click();
  await checkTheme('dark');
  assert.equal(await page.evaluate(key => localStorage.getItem(key), key), 'dark');
  await page.reload();
  await checkTheme('dark');
  await screenshot('portfolio-dark');

  const views = [
    ['reviews', '大V复盘日记'], ['stock', '个股分析'], ['portfolio', '持仓AI巡检'],
    ['limit-up', '短线连板'], ['themes', '趋势题材'], ['market', '行情总览'],
    ['mastery', '游资心法'], ['chat', 'AI 对话'], ['usage', '打开Token统计'],
  ];
  for (const [id, label] of views) {
    await page.locator('.app-sidebar').getByRole('button', { name: label, exact: true }).click();
    await checkTheme('dark');
    await screenshot(`${id}-dark`);
    await toggle().click();
    await checkTheme('light');
    await screenshot(`${id}-light`);
    await toggle().click();
  }
  await page.getByRole('button', { name: '打开系统设置', exact: true }).click();
  await page.getByRole('dialog').waitFor();
  await screenshot('settings-dark');
  await page.getByRole('button', { name: '关闭设置', exact: true }).click();

  await page.locator('.app-sidebar').getByRole('button', { name: '持仓AI巡检', exact: true }).click();
  for (const width of [1024, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await checkButtonFits();
    await screenshot(`portfolio-dark-${width}`);
    await toggle().focus();
    await page.keyboard.press('Enter');
    await checkTheme('light');
    await page.keyboard.press('Space');
    await checkTheme('dark');
  }

  const other = await context.newPage();
  await other.goto(`${base}/#portfolio-inspection`);
  await other.locator('.theme-toggle').waitFor();
  await other.locator('.theme-toggle').click();
  await checkTheme('light');
  await other.evaluate(() => localStorage.clear());
  await checkTheme('light');
  await other.close();

  const blocked = await browser.newContext();
  await blocked.addInitScript(() => {
    Object.defineProperty(window, 'localStorage', { get() { throw new Error('storage disabled'); } });
  });
  const fallback = await blocked.newPage();
  // Use a workspace that does not independently require draft storage.
  await fallback.goto(`${base}/#token-usage`);
  await fallback.locator('.theme-toggle').click();
  assert.equal(await fallback.locator('html').getAttribute('data-theme'), 'dark');
  await blocked.close();
  assert.deepEqual(errors, []);
  await fs.writeFile(path.join(output, 'result.json'), JSON.stringify({ passed: true, views: views.length, widths: [1440, 1024, 768, 390], checks: ['toggle', 'reload', 'cross-tab', 'keyboard', 'storage failure', 'settings', 'screenshots'], errors }, null, 2));
  console.log('Theme UI checks passed across 9 workspaces and 4 viewport widths.');
} finally {
  await browser.close();
}
