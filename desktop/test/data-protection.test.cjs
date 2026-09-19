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

test('update backup preserves models, articles, memories and login state byte-for-byte', async () => {
  const appData = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-backup-'));
  const userData = path.join(appData, 'easy-stock');
  fs.mkdirSync(userData);
  const fixtures = {
    'settings.json': '{"llm":{"provider":"openai","model":"gpt-test"}}',
    'reviews.db': Buffer.from([0x53, 0x51, 0x4c, 0x69, 0x74, 0x65, 0, 0xff]),
    'portfolio-inspections.db': 'portfolio-inspection-history',
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

  const backup = await createUpdateBackup({ userDataPath: userData, fromVersion: '0.3.0', toVersion: '0.4.0' });
  for (const [relativePath, content] of Object.entries(fixtures)) {
    assert.deepEqual(fs.readFileSync(path.join(backup.path, 'data', relativePath)), Buffer.from(content));
  }
  assert.equal(fs.existsSync(path.join(backup.path, 'data', 'Cache')), false);
  assert.equal(fs.existsSync(path.join(backup.path, 'data', 'Partitions', 'persist_xueqiu', 'Code Cache')), false);
  assert.equal(backup.manifest.files.length, Object.keys(fixtures).length);
  assert.deepEqual(backup.manifest.skipped, []);
  assert.equal(listUpdateBackups(backup.backupRoot).length, 1);
});

test('backup retries a transiently locked file and copies it once released', async () => {
  const appData = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-backup-'));
  const userData = path.join(appData, 'easy-stock');
  fs.mkdirSync(userData);
  write(userData, 'settings.json', '{"locked":false}');
  write(userData, 'reviews.db', 'db');

  const originalCopyFile = fs.copyFileSync;
  let busyAttempts = 0;
  fs.copyFileSync = function patchedCopyFile(sourcePath, targetPath) {
    if (String(sourcePath).endsWith('settings.json') && busyAttempts < 2) {
      busyAttempts += 1;
      const error = new Error(`EBUSY: resource busy or locked, copyfile '${sourcePath}' -> '${targetPath}'`);
      error.code = 'EBUSY';
      error.path = sourcePath;
      throw error;
    }
    return originalCopyFile(sourcePath, targetPath);
  };
  try {
    var backup = await createUpdateBackup({ userDataPath: userData, fromVersion: '0.3.0', toVersion: '0.4.0' });
  } finally {
    fs.copyFileSync = originalCopyFile;
  }
  assert.equal(busyAttempts, 2);
  assert.equal(fs.readFileSync(path.join(backup.path, 'data', 'settings.json'), 'utf8'), '{"locked":false}');
  assert.deepEqual(backup.manifest.skipped, []);
});

test('a permanently locked file is skipped and recorded without failing the backup', async () => {
  const appData = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-backup-'));
  const userData = path.join(appData, 'easy-stock');
  fs.mkdirSync(userData);
  write(userData, 'settings.json', '{"keep":true}');
  write(userData, 'logs/backend.log', 'log line');
  write(userData, 'hermes-home/memories/session.json', '{"memory":"keep"}');

  const originalCopyFile = fs.copyFileSync;
  fs.copyFileSync = function patchedCopyFile(sourcePath, targetPath) {
    if (String(sourcePath).endsWith('backend.log')) {
      const error = new Error(`EBUSY: resource busy or locked, copyfile '${sourcePath}' -> '${targetPath}'`);
      error.code = 'EBUSY';
      error.path = sourcePath;
      throw error;
    }
    return originalCopyFile(sourcePath, targetPath);
  };
  try {
    var backup = await createUpdateBackup({ userDataPath: userData, fromVersion: '0.9.1', toVersion: '1.2.0' });
  } finally {
    fs.copyFileSync = originalCopyFile;
  }
  assert.equal(fs.readFileSync(path.join(backup.path, 'data', 'settings.json'), 'utf8'), '{"keep":true}');
  assert.deepEqual(backup.manifest.files.map((item) => item.path).sort(), [
    path.join('hermes-home', 'memories', 'session.json'),
    'settings.json',
  ]);
  assert.equal(backup.manifest.skipped.length, 1);
  assert.equal(backup.manifest.skipped[0].path, path.join('logs', 'backend.log'));
  assert.equal(backup.manifest.skipped[0].type, 'skipped');
  assert.match(backup.manifest.skipped[0].reason, /EBUSY/);
  assert.doesNotMatch(backup.manifest.skipped[0].reason, /easy-stock-backup/);
});

test('a non-retryable copy error still fails the backup', async () => {
  const appData = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-backup-'));
  const userData = path.join(appData, 'easy-stock');
  fs.mkdirSync(userData);
  write(userData, 'settings.json', '{}');

  const originalCopyFile = fs.copyFileSync;
  fs.copyFileSync = function patchedCopyFile(sourcePath, targetPath) {
    const error = new Error(`ENOENT: no such file or directory, copyfile '${sourcePath}'`);
    error.code = 'ENOENT';
    error.path = sourcePath;
    throw error;
  };
  try {
    await assert.rejects(() => createUpdateBackup({ userDataPath: userData, fromVersion: '0.9.1', toVersion: '1.2.0' }), /ENOENT/);
  } finally {
    fs.copyFileSync = originalCopyFile;
  }
});

test('win32 backup survives a real exclusive file lock and records the skipped file', { skip: process.platform !== 'win32' }, async () => {
  const appData = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-backup-'));
  const userData = path.join(appData, 'easy-stock');
  fs.mkdirSync(userData);
  write(userData, 'settings.json', '{"real":"lock-test"}');
  const lockedFile = path.join(userData, 'hermes-home', 'session.db');
  write(userData, path.join('hermes-home', 'session.db'), '{"hermes":"session"}');

  const { spawn } = require('node:child_process');
  const locker = spawn('powershell.exe', [
    '-NoProfile', '-Command',
    `$fs=[System.IO.File]::Open('${lockedFile.replace(/'/g, "''")}','Open','Read','None'); Start-Sleep -Seconds 25; $fs.Close()`,
  ], { stdio: 'ignore', windowsHide: true });
  try {
    await new Promise((resolve, reject) => {
      let attempts = 0;
      const probe = () => {
        attempts += 1;
        const probeCopy = path.join(appData, 'probe.bin');
        try {
          fs.copyFileSync(lockedFile, probeCopy);
          fs.rmSync(probeCopy, { force: true });
          if (attempts > 80) {
            reject(new Error('test lock was never established'));
            return;
          }
          setTimeout(probe, 250);
        } catch {
          fs.rmSync(probeCopy, { force: true });
          resolve();
        }
      };
      probe();
    });

    const backup = await createUpdateBackup({ userDataPath: userData, fromVersion: '0.9.1', toVersion: '1.2.0' });
    assert.equal(fs.readFileSync(path.join(backup.path, 'data', 'settings.json'), 'utf8'), '{"real":"lock-test"}');
    assert.equal(backup.manifest.skipped.length, 1);
    assert.equal(backup.manifest.skipped[0].path, path.join('hermes-home', 'session.db'));
    assert.match(backup.manifest.skipped[0].reason, /EBUSY/);
  } finally {
    locker.kill();
  }
});

test('backup directory is outside user data and only the latest three backups remain', async () => {
  const appData = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-backup-'));
  const userData = path.join(appData, 'easy-stock');
  fs.mkdirSync(userData);
  write(userData, 'settings.json', '{}');
  const backupRoot = resolveBackupRoot(userData);
  assert.equal(backupRoot, path.join(appData, 'easy-stock-update-backups'));
  for (let day = 1; day <= 4; day += 1) {
    await createUpdateBackup({ userDataPath: userData, backupRoot, fromVersion: '0.3.0', toVersion: `0.3.${day}`, now: new Date(`2026-08-0${day}T00:00:00.000Z`) });
  }
  const backups = listUpdateBackups(backupRoot);
  assert.equal(backups.length, 3);
  assert.deepEqual(backups.map((item) => item.manifest.toVersion), ['0.3.4', '0.3.3', '0.3.2']);
});

test('rejects a backup root nested inside user data', () => {
  const userData = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-backup-'));
  assert.throws(() => resolveBackupRoot(userData, path.join(userData, 'backups')), /不能位于应用数据目录内/);
});
