// 资讯研判页 UI 验证：催化排行（前三徽章/热度条）+ 题材下钻（细分板块分组卡片）。
// 用法：先起 dev 后端（VITE_A_STOCK_BACKEND_URL 指向它）+ vite dev，然后
//   NEWS_UI_URL=http://127.0.0.1:20074 node scripts/verify-news-verdict-ui.mjs
// 截图输出到 .runtime/news-verdict-ui/（gitignored）。
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import path from 'node:path';

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.NEWS_UI_URL || 'http://127.0.0.1:20074';
const output = path.resolve(process.env.NEWS_UI_OUTPUT || '.runtime/news-verdict-ui');
await fs.mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true, channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome' });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
context.setDefaultTimeout(60000);
const page = await context.newPage();
const errors = [];
page.on('pageerror', error => errors.push(error.message));
const toggle = () => page.locator('.theme-toggle');
const setTheme = async theme => {
  const current = await page.evaluate(() => document.documentElement.dataset.theme);
  if (current !== theme) {
    await toggle().click();
    await page.waitForFunction(
      value => document.documentElement.dataset.theme === value,
      theme,
    );
  }
};
const screenshot = name => page.screenshot({ path: path.join(output, `${name}.png`), fullPage: true, animations: 'disabled' });

try {
  await page.goto(`${base}/#news-verdict`);
  await toggle().waitFor();

  // ---- 催化排行 ----
  await page.locator('.nv-rank-row').first().waitFor();
  await setTheme('light');
  const rankCount = await page.locator('.nv-rank-row').count();
  assert.ok(rankCount > 0, '催化排行应有题材行');
  const topBadges = await page.locator('.nv-rank-no-top').count();
  assert.ok(topBadges >= 1, '前三名应有徽章高亮');
  const heatBars = await page.locator('.nv-heat-bar').count();
  assert.equal(heatBars, rankCount, '每行应有热度条');
  await screenshot('rank-light');
  await setTheme('dark');
  await screenshot('rank-dark');
  await setTheme('light');

  // ---- 题材下钻：点第一行，等个股池 ----
  await page.locator('.nv-rank-row').first().click();
  await page.locator('.nv-drill-head').waitFor();
  await page.waitForFunction(
    () => document.querySelector('.nv-stock-group') || document.querySelector('.nv-stocks .nv-stock') || document.querySelector('.nv-stocks-block .nv-muted'),
    null,
    { timeout: 60000 },
  );
  const drillTitle = await page.locator('.nv-drill-title').textContent();
  assert.ok(drillTitle && drillTitle.trim().length > 0, '下钻标题不应为空');
  const groups = await page.locator('.nv-stock-group').count();
  const chips = await page.locator('.nv-stock').count();
  const matchSrc = await page.locator('.nv-match-src').count();
  console.log(`drill: title=${drillTitle.trim()} groups=${groups} chips=${chips} matchSrcPill=${matchSrc}`);
  await screenshot('drill-light');
  await setTheme('dark');
  await screenshot('drill-dark');

  assert.equal(errors.length, 0, `页面不应有 JS 错误: ${errors.join(' | ')}`);
  console.log(`OK: rank rows=${rankCount}, topBadges=${topBadges}, heatBars=${heatBars}, drill groups=${groups}, chips=${chips}`);
} finally {
  await browser.close();
}
