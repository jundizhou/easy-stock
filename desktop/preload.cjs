const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('aStock', {
  getBackendConfig: () => ipcRenderer.invoke('backend-config'),
	getRuntimeLogStatus: () => ipcRenderer.invoke('runtime-log-status'),
	openRuntimeLogs: () => ipcRenderer.invoke('runtime-open-logs'),
	logRuntimeEvent: (entry) => ipcRenderer.invoke('runtime-log', entry),
  getWechatServiceStatus: () => ipcRenderer.invoke('wechat-service-status'),
  openWechatLogin: () => ipcRenderer.invoke('open-wechat-login'),
  getBrowserAuthStatus: (profileId, source = 'xueqiu') => ipcRenderer.invoke('browser-auth-status', profileId, source),
  openReviewSourceLogin: (source, profileId, homepageURL) => ipcRenderer.invoke('open-review-source-login', source, profileId, homepageURL),
  openXueqiuLogin: (profileId, homepageURL) => ipcRenderer.invoke('open-xueqiu-login', profileId, homepageURL),
  getUpdateStatus: () => ipcRenderer.invoke('app-update-status'),
  checkForUpdates: () => ipcRenderer.invoke('app-update-check'),
  downloadUpdate: () => ipcRenderer.invoke('app-update-download'),
  installUpdate: () => ipcRenderer.invoke('app-update-install'),
  openUpdateRelease: () => ipcRenderer.invoke('app-update-open-release'),
  openUpdateBackups: () => ipcRenderer.invoke('app-update-open-backups'),
  onUpdateStatus: (listener) => {
    const handler = (_event, status) => listener(status);
    ipcRenderer.on('app-update-status-changed', handler);
    return () => ipcRenderer.removeListener('app-update-status-changed', handler);
  },
});
