const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('aStock', {
  getBackendConfig: () => ipcRenderer.invoke('backend-config'),
  getWechatServiceStatus: () => ipcRenderer.invoke('wechat-service-status'),
  openWechatLogin: () => ipcRenderer.invoke('open-wechat-login'),
  getBrowserAuthStatus: (profileId, source = 'xueqiu') => ipcRenderer.invoke('browser-auth-status', profileId, source),
  openReviewSourceLogin: (source, profileId, homepageURL) => ipcRenderer.invoke('open-review-source-login', source, profileId, homepageURL),
  openXueqiuLogin: (profileId, homepageURL) => ipcRenderer.invoke('open-xueqiu-login', profileId, homepageURL),
});
