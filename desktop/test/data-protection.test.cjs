const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const { createUpdateBackup, listUpdateBackups, resolveBackupRoot } = require('../data-protection.cjs');

function write(root, relativePath, content) {
  const target = path.join(root, relativePath);
  fs.mkdirSync(path.dirname(target), { recursive: true });
  fs.writeFileSync(target, content);
}

test('update backup preserves models, articles, memories and login state byte-for-byte', () => {
  const appData = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-backup-'));
  const userData = path.join(appData, 'easy-stock');
  fs.mkdirSync(userData);
  const fixtures = {
    'settings.json': '{"llm":{"provider":"openai","model":"gpt-test"}}',
    'reviews.db': Buffer.from([0x53, 0x51, 0x4c, 0x69, 0x74, 0x65, 0, 0xff]),
    'market-emotion.db': 'emotion-data',
    'theme-radar.db': 'theme-data',
    'hermes-home/.env': 'OPENAI_API_KEY=test-secret\n',
    'hermes-home/config.yaml': 'model: gpt-test\n',
    'hermes-home/memories/session.json': '{"memory":"keep"}',
    'hermes-workspace/imported/article.md': '# imported article',
    'browser-auth/xueqiu.json': '{"cookies":[{"name":"xq_a_token"}]}',
    'Partitions/persist_xueqiu/Cookies': Buffer.from([1, 2, 3, 4, 5]),
    'wechat-download-api/.env': 'WX_KEY=keep\n',
    'wechat-download-api/data/account.json': '{"fakeid":"123"}',
    'trading-mastery/index.json': '{"items":["keep"]}',
  };
  for (const [relativePath, content] of Object.entries(fixtures)) write(userData, relativePath, content);
  write(userData, 'Cache/remove.bin', 'cache');
  write(userData, 'Partitions/persist_xueqiu/Code Cache/remove.bin', 'cache');

  const backup = createUpdateBackup({ userDataPath: userData, fromVersion: '0.3.0', toVersion: '0.4.0' });
  for (const [relativePath, content] of Object.entries(fixtures)) {
    assert.deepEqual(fs.readFileSync(path.join(backup.path, 'data', relativePath)), Buffer.from(content));
  }
  assert.equal(fs.existsSync(path.join(backup.path, 'data', 'Cache')), false);
  assert.equal(fs.existsSync(path.join(backup.path, 'data', 'Partitions', 'persist_xueqiu', 'Code Cache')), false);
  assert.equal(backup.manifest.files.length, Object.keys(fixtures).length);
  assert.equal(listUpdateBackups(backup.backupRoot).length, 1);
});

test('backup directory is outside user data and only the latest three backups remain', () => {
  const appData = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-backup-'));
  const userData = path.join(appData, 'easy-stock');
  fs.mkdirSync(userData);
  write(userData, 'settings.json', '{}');
  const backupRoot = resolveBackupRoot(userData);
  assert.equal(backupRoot, path.join(appData, 'easy-stock-update-backups'));
  for (let day = 1; day <= 4; day += 1) {
    createUpdateBackup({ userDataPath: userData, backupRoot, fromVersion: '0.3.0', toVersion: `0.3.${day}`, now: new Date(`2026-08-0${day}T00:00:00.000Z`) });
  }
  const backups = listUpdateBackups(backupRoot);
  assert.equal(backups.length, 3);
  assert.deepEqual(backups.map((item) => item.manifest.toVersion), ['0.3.4', '0.3.3', '0.3.2']);
});

test('rejects a backup root nested inside user data', () => {
  const userData = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-backup-'));
  assert.throws(() => resolveBackupRoot(userData, path.join(userData, 'backups')), /不能位于应用数据目录内/);
});
