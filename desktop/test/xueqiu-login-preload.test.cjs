const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

test('review login preload exposes an explicit completion action for xueqiu and taoguba', () => {
  const source = fs.readFileSync(path.resolve(__dirname, '..', 'review-login-preload.cjs'), 'utf8');
  assert.match(source, /我已完成登录/);
  assert.match(source, /淘股吧/);
  assert.match(source, /ipcRenderer\.invoke\('review-browser-login-complete'\)/);
  assert.match(source, /status\?\.configured/);
});
