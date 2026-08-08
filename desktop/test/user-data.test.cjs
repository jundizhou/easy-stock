const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const { resolveUserDataPath } = require('../user-data.cjs');

test('uses legacy desktop data when renamed directory has no settings', () => {
  const appDataPath = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-user-data-'));
  const currentUserDataPath = path.join(appDataPath, 'easy-stock');
  const legacyUserDataPath = path.join(appDataPath, 'desktop');
  fs.mkdirSync(currentUserDataPath);
  fs.mkdirSync(legacyUserDataPath);
  fs.writeFileSync(path.join(legacyUserDataPath, 'settings.json'), '{}');

  assert.equal(resolveUserDataPath({ appDataPath, currentUserDataPath }), legacyUserDataPath);
});

test('prefers easy-stock data once it has settings', () => {
  const appDataPath = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-user-data-'));
  const currentUserDataPath = path.join(appDataPath, 'easy-stock');
  const legacyUserDataPath = path.join(appDataPath, 'desktop');
  fs.mkdirSync(currentUserDataPath);
  fs.mkdirSync(legacyUserDataPath);
  fs.writeFileSync(path.join(currentUserDataPath, 'settings.json'), '{}');
  fs.writeFileSync(path.join(legacyUserDataPath, 'settings.json'), '{}');

  assert.equal(resolveUserDataPath({ appDataPath, currentUserDataPath }), currentUserDataPath);
});
