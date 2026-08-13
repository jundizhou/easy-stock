const assert = require('node:assert/strict');
const { EventEmitter } = require('node:events');
const test = require('node:test');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const { UpdateManager } = require('../update-manager.cjs');
const { createUpdateBackup } = require('../data-protection.cjs');

class FakeUpdater extends EventEmitter {
  async checkForUpdates() { this.emit('checking-for-update'); }
  async downloadUpdate() {}
  quitAndInstall(...args) { this.installArgs = args; }
}

test('updater transitions from available through downloaded and installs after backup', async () => {
  const updater = new FakeUpdater();
  const calls = [];
  const manager = new UpdateManager({
    updater,
    enabled: true,
    currentVersion: '0.3.0',
    platform: 'win32',
    stopRuntime: async () => calls.push('stop'),
    createBackup: async (versions) => {
      calls.push(`backup:${versions.fromVersion}:${versions.toVersion}`);
      return { path: '/backups/test', manifest: { createdAt: '2026-08-12T00:00:00.000Z' } };
    },
  });

  updater.emit('update-available', { version: '0.4.0', releaseName: 'Update', releaseNotes: 'notes' });
  assert.equal(manager.getStatus().state, 'available');
  await manager.downloadUpdate();
  updater.emit('download-progress', { percent: 42, transferred: 42, total: 100, bytesPerSecond: 10 });
  assert.equal(manager.getStatus().progress, 42);
  updater.emit('update-downloaded', { version: '0.4.0' });
  await manager.installUpdate();

  assert.deepEqual(calls, ['stop', 'backup:0.3.0:0.4.0']);
  assert.deepEqual(updater.installArgs, [false, true]);
  assert.equal(manager.getStatus().backupPath, '/backups/test');
});

test('backup failure prevents quitAndInstall', async () => {
  const updater = new FakeUpdater();
  const manager = new UpdateManager({
    updater,
    enabled: true,
    currentVersion: '0.3.0',
    platform: 'win32',
    stopRuntime: async () => {},
    createBackup: async () => { throw new Error('/Users/name/private backup failed'); },
    logger: { error() {} },
  });
  updater.emit('update-available', { version: '0.4.0' });
  updater.emit('update-downloaded', { version: '0.4.0' });

  await assert.rejects(() => manager.installUpdate(), /backup failed/);
  assert.equal(updater.installArgs, undefined);
  assert.equal(manager.getStatus().state, 'error');
  assert.match(manager.getStatus().message, /\[本机路径\]/);
});

test('development updater is disabled', async () => {
  const manager = new UpdateManager({ enabled: false, currentVersion: '0.3.0' });
  assert.equal(manager.getStatus().state, 'disabled');
  await assert.rejects(() => manager.checkForUpdates(), /不支持自动更新/);
});

test('unsigned macOS updater stays manual and never invokes native download or install', async () => {
  const updater = new FakeUpdater();
  let downloaded = false;
  updater.downloadUpdate = async () => { downloaded = true; };
  const manager = new UpdateManager({
    updater,
    enabled: true,
    currentVersion: '0.4.0',
    platform: 'darwin',
    stopRuntime: async () => { throw new Error('should not stop runtime'); },
    createBackup: async () => { throw new Error('should not backup'); },
  });
  assert.equal(manager.getStatus().installMode, 'manual');
  updater.emit('update-available', { version: '0.5.0' });
  assert.equal(manager.getStatus().message, '发现新版本 v0.5.0，请前往发布页下载安装');
  await assert.rejects(() => manager.downloadUpdate(), /Apple Developer ID/);
  assert.equal(downloaded, false);
  updater.emit('update-downloaded', { version: '0.5.0' });
  await assert.rejects(() => manager.installUpdate(), /Apple Developer ID/);
  assert.equal(updater.installArgs, undefined);
});

test('signature validation errors are translated to a useful macOS message', () => {
  const updater = new FakeUpdater();
  const manager = new UpdateManager({
    updater,
    enabled: true,
    currentVersion: '0.4.0',
    platform: 'darwin',
    logger: { error() {} },
  });
  updater.emit('error', new Error('Code signature at URL file:/tmp/EasyStock.zip did not pass validation: code requirement failed'));
  assert.match(manager.getStatus().message, /Apple Developer ID/);
  assert.doesNotMatch(manager.getStatus().message, /\/tmp/);
});

test('end-to-end updater backup preserves real user data before install', async () => {
  const appData = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-update-e2e-'));
  const userData = path.join(appData, 'easy-stock');
  fs.mkdirSync(path.join(userData, 'hermes-home'), { recursive: true });
  fs.mkdirSync(path.join(userData, 'browser-auth'), { recursive: true });
  fs.writeFileSync(path.join(userData, 'settings.json'), '{"model":"test"}');
  fs.writeFileSync(path.join(userData, 'reviews.db'), Buffer.from([0, 1, 2, 255]));
  fs.writeFileSync(path.join(userData, 'hermes-home', '.env'), 'API_KEY=secret\n');
  fs.writeFileSync(path.join(userData, 'browser-auth', 'xueqiu.json'), '{"cookie":"keep"}');
  const updater = new FakeUpdater();
  const manager = new UpdateManager({
    updater,
    enabled: true,
    currentVersion: '0.3.0',
    platform: 'win32',
    stopRuntime: async () => {},
    createBackup: ({ fromVersion, toVersion }) => createUpdateBackup({ userDataPath: userData, fromVersion, toVersion }),
  });
  updater.emit('update-available', { version: '0.4.0' });
  updater.emit('update-downloaded', { version: '0.4.0' });
  await manager.installUpdate();

  const backupData = path.join(manager.getStatus().backupPath, 'data');
  for (const relativePath of ['settings.json', 'reviews.db', 'hermes-home/.env', 'browser-auth/xueqiu.json']) {
    assert.deepEqual(fs.readFileSync(path.join(backupData, relativePath)), fs.readFileSync(path.join(userData, relativePath)));
  }
  assert.deepEqual(updater.installArgs, [false, true]);
});
