const { EventEmitter } = require('node:events');

function readableError(error) {
  const message = error instanceof Error ? error.message : String(error || '未知错误');
  return message.replace(/(?:[A-Za-z]:\\|\/)[^\s)]+/g, '[本机路径]');
}

class UpdateManager extends EventEmitter {
  constructor({ updater, enabled, currentVersion, platform, canInstallAutomatically, stopRuntime, createBackup, logger = console }) {
    super();
    this.updater = updater;
    this.enabled = Boolean(enabled && updater);
    this.currentVersion = currentVersion;
    this.platform = platform;
    // Unsigned macOS builds cannot be replaced by Squirrel.Mac: the native
    // updater validates the code signature before installation. Keep checking
    // updates enabled, but route those builds to the release page instead.
    this.canInstallAutomatically = canInstallAutomatically ?? platform === 'win32';
    this.installMode = this.canInstallAutomatically ? 'automatic' : 'manual';
    this.stopRuntime = stopRuntime;
    this.createBackup = createBackup;
    this.logger = logger;
    this.latestInfo = null;
    this.status = {
      state: this.enabled ? 'idle' : 'disabled',
      supported: this.enabled,
      installMode: this.installMode,
      currentVersion,
      message: this.enabled ? '可检查正式更新源的新版本' : '开发模式不启用自动更新',
      progress: 0,
    };
    if (this.enabled) this.bindUpdater();
  }

  bindUpdater() {
    this.updater.autoDownload = false;
    this.updater.autoInstallOnAppQuit = false;
    this.updater.allowPrerelease = false;
    this.updater.on('checking-for-update', () => this.setStatus({ state: 'checking', message: '正在检查新版本', progress: 0 }));
    this.updater.on('update-available', (info) => {
      this.latestInfo = info;
      this.setStatus({
        state: 'available',
        latestVersion: info.version,
        releaseName: info.releaseName || '',
        releaseNotes: normalizeReleaseNotes(info.releaseNotes),
        message: this.canInstallAutomatically ? `发现新版本 v${info.version}` : `发现新版本 v${info.version}，请前往发布页下载安装`,
        progress: 0,
      });
    });
    this.updater.on('update-not-available', (info) => {
      this.latestInfo = info || null;
      this.setStatus({ state: 'not-available', latestVersion: info?.version || this.currentVersion, message: '当前已是最新版本', progress: 0 });
    });
    this.updater.on('download-progress', (progress) => {
      this.setStatus({
        state: 'downloading',
        progress: Math.max(0, Math.min(100, Number(progress.percent) || 0)),
        transferred: Number(progress.transferred) || 0,
        total: Number(progress.total) || 0,
        bytesPerSecond: Number(progress.bytesPerSecond) || 0,
        message: `正在下载 v${this.latestInfo?.version || ''}`.trim(),
      });
    });
    this.updater.on('update-downloaded', (info) => {
      this.latestInfo = info || this.latestInfo;
      this.setStatus({
        state: 'downloaded',
        latestVersion: info?.version || this.latestInfo?.version,
        releaseName: info?.releaseName || this.status.releaseName || '',
        releaseNotes: normalizeReleaseNotes(info?.releaseNotes) || this.status.releaseNotes || '',
        progress: 100,
        message: this.canInstallAutomatically ? '更新已下载，可重启安装' : '请前往发布页手动下载安装新版',
      });
    });
    this.updater.on('error', (error) => {
      this.logger.error?.('[updater]', error);
      this.setStatus({ state: 'error', message: `更新失败：${friendlyUpdateError(error)}` });
    });
  }

  setStatus(patch) {
    this.status = { ...this.status, ...patch, supported: this.enabled, installMode: this.installMode, currentVersion: this.currentVersion };
    this.emit('status', this.getStatus());
    return this.getStatus();
  }

  getStatus() {
    return { ...this.status };
  }

  async checkForUpdates() {
    this.ensureEnabled();
    try {
      await this.updater.checkForUpdates();
      return this.getStatus();
    } catch (error) {
      this.setStatus({ state: 'error', message: `检查更新失败：${friendlyUpdateError(error)}` });
      throw new Error(this.status.message);
    }
  }

  async downloadUpdate() {
    this.ensureEnabled();
    if (!this.canInstallAutomatically) {
      throw new Error('当前 macOS 版本未使用 Apple Developer ID 签名，请前往发布页手动下载安装新版');
    }
    if (!['available', 'error'].includes(this.status.state) || !this.latestInfo?.version) {
      throw new Error('当前没有可下载的新版本');
    }
    this.setStatus({ state: 'downloading', progress: 0, message: `准备下载 v${this.latestInfo.version}` });
    try {
      await this.updater.downloadUpdate();
      return this.getStatus();
    } catch (error) {
      this.setStatus({ state: 'error', message: `下载更新失败：${friendlyUpdateError(error)}` });
      throw new Error(this.status.message);
    }
  }

  async installUpdate() {
    this.ensureEnabled();
    if (!this.canInstallAutomatically) {
      throw new Error('当前 macOS 版本未使用 Apple Developer ID 签名，暂不支持应用内安装，请前往发布页手动下载安装新版');
    }
    if (this.status.state !== 'downloaded' || !this.latestInfo?.version) {
      throw new Error('更新尚未下载完成');
    }
    this.setStatus({ state: 'installing', message: '正在停止本机服务并备份数据' });
    try {
      await this.stopRuntime();
      const backup = await this.createBackup({ fromVersion: this.currentVersion, toVersion: this.latestInfo.version });
      this.setStatus({ state: 'installing', backupPath: backup.path, backupCreatedAt: backup.manifest?.createdAt, message: '数据备份完成，正在启动安装程序' });
      this.updater.quitAndInstall(false, true);
      return this.getStatus();
    } catch (error) {
      this.setStatus({ state: 'error', message: `安装已中止：${friendlyUpdateError(error)}。请重新启动应用后再试。` });
      throw new Error(this.status.message);
    }
  }

  ensureEnabled() {
    if (!this.enabled) throw new Error('当前环境不支持自动更新');
  }
}

function normalizeReleaseNotes(value) {
  if (Array.isArray(value)) return value.map((item) => item.note || '').filter(Boolean).join('\n\n');
  return typeof value === 'string' ? value : '';
}

function friendlyUpdateError(error) {
  const message = readableError(error);
  if (/code signature|代码未能满足指定的代码要求|did not pass validation/i.test(message)) {
    return 'macOS 校验到当前安装包未使用一致的 Apple Developer ID 签名，请前往发布页手动下载安装新版';
  }
  return message;
}

module.exports = { UpdateManager, normalizeReleaseNotes, readableError, friendlyUpdateError };
