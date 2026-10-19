const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const desktopRoot = path.resolve(__dirname, '..');

// 打包时的 asar 文件白名单是显式的：主进程 require 了但没进名单的模块，
// 在打包版里会直接抛 MODULE_NOT_FOUND（开发模式完全看不出来）。
test('packaged asar file list covers every module the main process requires', () => {
  const main = fs.readFileSync(path.join(desktopRoot, 'main.cjs'), 'utf8');
  const builder = fs.readFileSync(path.join(desktopRoot, 'scripts', 'electron-builder.mjs'), 'utf8');

  const block = builder.match(/files:\s*\[([\s\S]*?)\]/);
  assert.ok(block, 'electron-builder.mjs should declare an explicit files allow-list');
  const listed = new Set(
    [...block[1].matchAll(/'([^']+)'/g)].map((match) => match[1]).filter((entry) => !entry.startsWith('!')),
  );

  const required = new Set([...main.matchAll(/require\('(\.\/[^']+\.cjs)'\)/g)].map((match) => match[1].slice(2)));
  assert.ok(required.size > 0, 'main.cjs should require local modules');

  for (const name of required) {
    assert.ok(fs.existsSync(path.join(desktopRoot, name)), `${name} should exist next to main.cjs`);
    assert.ok(listed.has(name), `${name} is required by main.cjs but missing from the packaged files list`);
  }
});

test('packaged asar file list only names existing modules', () => {
  const builder = fs.readFileSync(path.join(desktopRoot, 'scripts', 'electron-builder.mjs'), 'utf8');
  const block = builder.match(/files:\s*\[([\s\S]*?)\]/);
  assert.ok(block, 'electron-builder.mjs should declare an explicit files allow-list');

  for (const match of block[1].matchAll(/'([^']+\.cjs)'/g)) {
    assert.ok(fs.existsSync(path.join(desktopRoot, match[1])), `${match[1]} is listed for packaging but does not exist`);
  }
});
