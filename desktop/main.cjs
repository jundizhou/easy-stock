const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const { app, BrowserWindow, ipcMain, session, shell } = require('electron');
const { autoUpdater } = require('electron-updater');
const {
  findFreePort,
  resolveBackendCommand,
  startBackend,
  waitForHealth,
} = require('./backend-process.cjs');
const {
  LEGACY_TAOGUBA_AUTH_MARKER,
  TAOGUBA_AUTH_MARKER,
  partitionForProfile,
  playwrightCookie,
  readBrowserAuthStatus,
  statePathForProfile,
  writeStorageState,
} = require('./browser-auth.cjs');
const { createTaogubaBrowserBridge } = require('./taoguba-browser-bridge.cjs');
const { createXueqiuBrowserBridge } = require('./xueqiu-browser-bridge.cjs');
const {
  readWechatServiceStatus,
  resolveWechatPython,
  startWechatService,
  syncWechatServiceSource,
  waitForWechatHealth,
} = require('./wechat-service.cjs');
const { resolveUserDataPath } = require('./user-data.cjs');
const { resolveHermesRuntimeRoot } = require('./hermes-runtime-root.cjs');
const { createUpdateBackup, resolveBackupRoot } = require('./data-protection.cjs');
const { UpdateManager } = require('./update-manager.cjs');
const { resolveUpdateFeedURL } = require('./update-feed.cjs');
const { createRotatingLogger } = require('./runtime-logger.cjs');

app.setName('easy-stock');
const defaultUserDataPath = app.getPath('userData');
const selectedUserDataPath = resolveUserDataPath({
  appDataPath: app.getPath('appData'),
  currentUserDataPath: defaultUserDataPath,
  configuredPath: process.env.A_STOCK_USER_DATA_DIR,
});
if (selectedUserDataPath !== defaultUserDataPath) {
  app.setPath('userData', selectedUserDataPath);
}

const runtimeLogDirectory = path.resolve(process.env.A_STOCK_LOG_DIR || path.join(app.getPath('userData'), 'logs'));
fs.mkdirSync(runtimeLogDirectory, { recursive: true, mode: 0o700 });
app.setAppLogsPath(runtimeLogDirectory);
const desktopLogger = createRotatingLogger({
  directory: runtimeLogDirectory,
  fileName: 'desktop.log',
  component: 'desktop',
  mirror: console,
});
const rendererLogger = createRotatingLogger({
  directory: runtimeLogDirectory,
  fileName: 'renderer.log',
  component: 'renderer',
  mirror: console,
});

desktopLogger.event('info', 'runtime', `started version=${app.getVersion()} packaged=${app.isPackaged} platform=${process.platform} arch=${process.arch}`);
process.on('uncaughtExceptionMonitor', (error, origin) => desktopLogger.event('error', 'runtime', `uncaught exception origin=${origin}`, error));
process.on('unhandledRejection', (reason) => desktopLogger.event('error', 'runtime', 'unhandled rejection', reason));

let backendProcess;
let backendConfig;
let xueqiuBrowserBridge;
let xueqiuBrowserBridgeConfig;
let taogubaBrowserBridge;
let taogubaBrowserBridgeConfig;
let wechatServiceProcess;
let wechatServiceConfig;
let wechatServiceError = '';
let updateManager;
let updateCheckTimer;
const reviewLoginWindows = new Map();

function featureLogger(feature) {
  return {
    debug: (...values) => desktopLogger.event('debug', feature, ...values),
    info: (...values) => desktopLogger.event('info', feature, ...values),
    log: (...values) => desktopLogger.event('info', feature, ...values),
    warn: (...values) => desktopLogger.event('warn', feature, ...values),
    error: (...values) => desktopLogger.event('error', feature, ...values),
  };
}

function logChildOutput(feature, level, chunk) {
  const message = String(chunk || '').trim();
  if (message) desktopLogger.event(level, feature, message);
}

function resourcesRoot() {
  return app.isPackaged
    ? path.join(process.resourcesPath, 'resources')
    : path.join(__dirname, 'resources');
}

async function bootWechatService() {
	desktopLogger.event('info', 'wechat-service', 'starting');
  const bundledRoot = resourcesRoot();
  const sourceDir = process.env.A_STOCK_WECHAT_SERVICE_SOURCE || path.join(bundledRoot, 'wechat-download-api');
  const workDir = process.env.A_STOCK_WECHAT_SERVICE_HOME || path.join(app.getPath('userData'), 'wechat-download-api');
  const runtimeRoot = process.env.A_STOCK_HERMES_RUNTIME_ROOT || path.join(bundledRoot, 'hermes-runtime');
  const python = process.env.A_STOCK_WECHAT_PYTHON || resolveWechatPython(runtimeRoot);
  syncWechatServiceSource({ sourceDir, workDir });

  const port = await findFreePort('127.0.0.1', 30000, 39999);
  const baseURL = `http://127.0.0.1:${port}`;
  wechatServiceProcess = startWechatService({ python, workDir, port, baseURL });
	wechatServiceProcess.stdout?.on('data', (chunk) => logChildOutput('wechat-service', 'info', chunk));
	wechatServiceProcess.stderr?.on('data', (chunk) => logChildOutput('wechat-service', 'warn', chunk));
	wechatServiceProcess.once('error', (error) => desktopLogger.event('error', 'wechat-service', 'process error', error));
  wechatServiceProcess.once('exit', (code, signal) => {
		desktopLogger.event(app.isQuitting ? 'info' : 'error', 'wechat-service', `process exited code=${code ?? 'none'} signal=${signal || 'none'} quitting=${Boolean(app.isQuitting)}`);
    if (!app.isQuitting) {
      wechatServiceError = `内置微信公众号服务已停止（${signal || `退出码 ${code}`}）`;
			desktopLogger.event('error', 'wechat-service', wechatServiceError);
    }
  });
  await waitForWechatHealth(baseURL);
  wechatServiceConfig = { baseURL, workDir };
  wechatServiceError = '';
	desktopLogger.event('info', 'wechat-service', 'ready');
  return wechatServiceConfig;
}

async function bootBackend() {
	desktopLogger.event('info', 'backend-process', 'starting');
  const port = await findFreePort();
  const token = crypto.randomBytes(24).toString('hex');
  const addr = `127.0.0.1:${port}`;
  const backendUrl = `http://${addr}`;
  const developmentBackendDir = path.resolve(__dirname, '..', 'backend');
  const devBinary = path.join(__dirname, 'bin', 'easy-stock-backend');
  const bundledRoot = resourcesRoot();
  const executableName = process.platform === 'win32' ? 'easy-stock-backend.exe' : 'easy-stock-backend';
  const packagedBinary = path.join(bundledRoot, 'backend', executableName);
  const backendDir = app.isPackaged ? path.dirname(packagedBinary) : developmentBackendDir;
  const configuredBinary = process.env.A_STOCK_BACKEND_BIN || '';
  const backendBin = [configuredBinary, app.isPackaged ? packagedBinary : devBinary].find((candidate) => candidate && fs.existsSync(candidate)) || '';
  const command = resolveBackendCommand({
    backendBin,
    backendDir,
    isPackaged: app.isPackaged,
  });

  backendProcess = startBackend({
    ...command,
    addr,
    token,
    extraEnv: buildRuntimeEnv(bundledRoot),
  });
	backendProcess.stdout?.on('data', (chunk) => logChildOutput('backend-process', 'info', chunk));
	backendProcess.stderr?.on('data', (chunk) => logChildOutput('backend-process', 'warn', chunk));
	backendProcess.once('error', (error) => desktopLogger.event('error', 'backend-process', 'process error', error));
	backendProcess.once('exit', (code, signal) => desktopLogger.event(app.isQuitting ? 'info' : 'error', 'backend-process', `process exited code=${code ?? 'none'} signal=${signal || 'none'} quitting=${Boolean(app.isQuitting)}`));

  await waitForHealth(backendUrl);
  backendConfig = { backendUrl, token };
	desktopLogger.event('info', 'backend-process', 'ready');
  return backendConfig;
}

async function createWindow() {
  await Promise.all([
    bootXueqiuBrowserBridge(),
    bootTaogubaBrowserBridge(),
    bootWechatService().catch((error) => {
      wechatServiceError = error.message || String(error);
			desktopLogger.event('error', 'wechat-service', 'startup failed', error);
      if (wechatServiceProcess && !wechatServiceProcess.killed) wechatServiceProcess.kill();
      wechatServiceProcess = undefined;
      wechatServiceConfig = undefined;
    }),
  ]);
  await bootBackend();
	const windowIcon = path.join(__dirname, 'assets', 'easy-stock.png');

  const window = new BrowserWindow({
    width: 1280,
    height: 860,
    minWidth: 980,
    minHeight: 680,
		title: 'easy-stock · AI A股投研工作台 · 仅限个人非商业使用',
		...(fs.existsSync(windowIcon) ? { icon: windowIcon } : {}),
    backgroundColor: '#f4f5f7',
    webPreferences: {
      preload: path.join(__dirname, 'preload.cjs'),
      contextIsolation: true,
      nodeIntegration: false,
      webviewTag: true,
    },
  });
  window.webContents.on('will-attach-webview', (event, webPreferences, params) => {
    let allowed = false;
    try {
      const source = new URL(params.src);
      const service = wechatServiceConfig ? new URL(wechatServiceConfig.baseURL) : null;
      allowed = Boolean(service && source.origin === service.origin && source.pathname === '/login.html');
    } catch {
      allowed = false;
    }
    if (!allowed) {
      event.preventDefault();
      return;
    }
    delete webPreferences.preload;
    webPreferences.nodeIntegration = false;
    webPreferences.contextIsolation = true;
    webPreferences.sandbox = true;
  });
	window.webContents.on('did-fail-load', (_event, errorCode, errorDescription, validatedURL, isMainFrame) => {
		if (isMainFrame) desktopLogger.event('error', 'renderer', `load failed code=${errorCode} description=${errorDescription} path=${safeURLPath(validatedURL)}`);
	});
	window.webContents.on('render-process-gone', (_event, details) => {
		desktopLogger.event('error', 'renderer', `process gone reason=${details.reason} exit_code=${details.exitCode}`);
	});

  const rendererURL = process.env.ELECTRON_RENDERER_URL;
  if (rendererURL) {
    await window.loadURL(rendererURL);
    return;
  }
  const frontendPath = app.isPackaged
    ? path.join(process.resourcesPath, 'resources', 'frontend', 'dist', 'index.html')
    : path.resolve(__dirname, '..', 'frontend', 'dist', 'index.html');
  await window.loadFile(frontendPath);
}

function terminateChild(child, timeoutMs = 10000) {
  if (!child || child.killed || child.exitCode !== null) return Promise.resolve();
  return new Promise((resolve, reject) => {
    let settled = false;
    let timer;
    let forceTimer;
    const finish = () => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      clearTimeout(forceTimer);
      resolve();
    };
    child.once('exit', finish);
    child.kill();
    timer = setTimeout(() => {
      if (child.exitCode !== null) return finish();
      try { child.kill('SIGKILL'); } catch {}
      forceTimer = setTimeout(() => {
        if (child.exitCode !== null) return finish();
        if (settled) return;
        settled = true;
        reject(new Error('本机后台服务未能及时停止'));
      }, 2000);
    }, timeoutMs);
  });
}

async function stopRuntime() {
  const loginSessions = [...reviewLoginWindows.values()]
    .filter((entry) => !entry.window.isDestroyed())
    .map((entry) => entry.window.webContents.session);
  await Promise.all([
    session.defaultSession?.flushStorageData(),
    ...loginSessions.map((persistentSession) => persistentSession.flushStorageData()),
  ].filter(Boolean));
  for (const entry of reviewLoginWindows.values()) {
    if (!entry.window.isDestroyed()) entry.window.destroy();
  }
  reviewLoginWindows.clear();
  await Promise.all([
    xueqiuBrowserBridge?.close(),
    taogubaBrowserBridge?.close(),
    terminateChild(backendProcess),
    terminateChild(wechatServiceProcess),
  ].filter(Boolean));
  xueqiuBrowserBridge = undefined;
  xueqiuBrowserBridgeConfig = undefined;
  taogubaBrowserBridge = undefined;
  taogubaBrowserBridgeConfig = undefined;
  backendProcess = undefined;
  backendConfig = undefined;
  wechatServiceProcess = undefined;
  wechatServiceConfig = undefined;
}

function initializeUpdateManager() {
  const enabled = app.isPackaged && ['darwin', 'win32'].includes(process.platform);
  if (enabled) {
    autoUpdater.setFeedURL({ provider: 'generic', url: resolveUpdateFeedURL() });
  }
  updateManager = new UpdateManager({
    updater: autoUpdater,
    enabled,
    currentVersion: app.getVersion(),
    platform: process.platform,
    canInstallAutomatically: process.platform === 'win32',
    stopRuntime,
    createBackup: ({ fromVersion, toVersion }) => createUpdateBackup({
      userDataPath: app.getPath('userData'),
      backupRoot: resolveBackupRoot(app.getPath('userData')),
      fromVersion,
      toVersion,
    }),
		logger: featureLogger('updater'),
  });
  updateManager.on('status', (status) => {
    for (const window of BrowserWindow.getAllWindows()) {
      if (!window.isDestroyed()) window.webContents.send('app-update-status-changed', status);
    }
  });
  if (enabled) {
    setTimeout(() => void updateManager.checkForUpdates().catch(() => {}), 30000).unref?.();
    updateCheckTimer = setInterval(() => void updateManager.checkForUpdates().catch(() => {}), 12 * 60 * 60 * 1000);
    updateCheckTimer.unref?.();
  }
}

function buildRuntimeEnv(resourcesRoot) {
  const userData = app.getPath('userData');
  const hermesHome = process.env.A_STOCK_HERMES_HOME || path.join(userData, 'hermes-home');
  const hermesWorkDir = process.env.A_STOCK_HERMES_WORKDIR || path.join(userData, 'hermes-workspace');
  const browserStateDir = process.env.A_STOCK_BROWSER_STATE_DIR || path.join(userData, 'browser-auth');
  const bundledRuntime = path.join(resourcesRoot, 'hermes-runtime');
  const packagedBrowserWrapperDir = path.join(resourcesRoot, 'agent-browser');
  const developmentBrowserWrapperDir = path.join(__dirname, 'scripts', 'browser-bin');
  const browserWrapperDir = fs.existsSync(path.join(packagedBrowserWrapperDir, 'agent-browser'))
    ? packagedBrowserWrapperDir
    : developmentBrowserWrapperDir;
  const packagedBrowserReal = path.join(packagedBrowserWrapperDir, process.platform === 'win32' ? 'agent-browser-real.exe' : 'agent-browser-real');
  const developmentBrowserReal = path.resolve(__dirname, '..', 'node_modules', 'agent-browser', 'bin', agentBrowserBinaryName());
  const browserReal = fs.existsSync(packagedBrowserReal) ? packagedBrowserReal : developmentBrowserReal;
  const hermesRuntimeRoot = resolveHermesRuntimeRoot({
    configuredRoot: process.env.A_STOCK_HERMES_RUNTIME_ROOT,
    bundledRoot: resourcesRoot,
    projectRoot: path.resolve(__dirname, '..'),
  });
  fs.mkdirSync(hermesHome, { recursive: true });
  fs.mkdirSync(hermesWorkDir, { recursive: true });
  fs.mkdirSync(browserStateDir, { recursive: true, mode: 0o700 });
  return {
		A_STOCK_LOG_DIR: runtimeLogDirectory,
		A_STOCK_APP_VERSION: app.getVersion(),
    A_STOCK_SETTINGS_PATH: path.join(userData, 'settings.json'),
    A_STOCK_REVIEW_DB: path.join(userData, 'reviews.db'),
    A_STOCK_PORTFOLIO_DB: path.join(userData, 'portfolio-inspections.db'),
    A_STOCK_MARKET_EMOTION_DB: path.join(userData, 'market-emotion.db'),
    A_STOCK_THEME_RADAR_DB: path.join(userData, 'theme-radar.db'),
    A_STOCK_MASTERY_CACHE: path.join(userData, 'trading-mastery'),
    A_STOCK_HERMES_HOME: hermesHome,
    A_STOCK_HERMES_WORKDIR: hermesWorkDir,
    A_STOCK_HERMES_RUNTIME_ROOT: hermesRuntimeRoot,
    A_STOCK_BROWSER_STATE_DIR: browserStateDir,
    ...(xueqiuBrowserBridgeConfig ? {
      A_STOCK_BROWSER_BRIDGE_URL: xueqiuBrowserBridgeConfig.baseURL,
      A_STOCK_BROWSER_BRIDGE_TOKEN: xueqiuBrowserBridgeConfig.token,
    } : {}),
    ...(taogubaBrowserBridgeConfig ? {
      A_STOCK_TAOGUBA_BROWSER_BRIDGE_URL: taogubaBrowserBridgeConfig.baseURL,
      A_STOCK_TAOGUBA_BROWSER_BRIDGE_TOKEN: taogubaBrowserBridgeConfig.token,
    } : {}),
    ...(wechatServiceConfig ? { A_STOCK_WECHAT_API_URL: wechatServiceConfig.baseURL } : {}),
    A_STOCK_AGENT_BROWSER_WRAPPER_DIR: browserWrapperDir,
    A_STOCK_AGENT_BROWSER_REAL: browserReal,
    ...(process.env.A_STOCK_HERMES_PYTHON ? { A_STOCK_HERMES_PYTHON: process.env.A_STOCK_HERMES_PYTHON } : {}),
  };
}

async function bootXueqiuBrowserBridge() {
  if (xueqiuBrowserBridgeConfig) return xueqiuBrowserBridgeConfig;
	xueqiuBrowserBridge = createXueqiuBrowserBridge({
    BrowserWindow,
    partitionForProfile: (profileId) => partitionForProfile('xueqiu', profileId),
		logger: featureLogger('xueqiu-bridge'),
  });
  xueqiuBrowserBridgeConfig = await xueqiuBrowserBridge.start();
  return xueqiuBrowserBridgeConfig;
}

async function bootTaogubaBrowserBridge() {
  if (taogubaBrowserBridgeConfig) return taogubaBrowserBridgeConfig;
	taogubaBrowserBridge = createTaogubaBrowserBridge({
    BrowserWindow,
    partitionForProfile: (profileId) => partitionForProfile('taoguba', profileId),
		logger: featureLogger('taoguba-bridge'),
  });
  taogubaBrowserBridgeConfig = await taogubaBrowserBridge.start();
  return taogubaBrowserBridgeConfig;
}

function agentBrowserBinaryName() {
  if (process.platform === 'win32') return 'agent-browser-win32-x64.exe';
  return `agent-browser-${process.platform}-${process.arch}`;
}

ipcMain.handle('backend-config', () => backendConfig);
ipcMain.handle('runtime-log-status', () => ({
	available: true,
	directory: runtimeLogDirectory,
	max_file_mb: 5,
	backup_files: 5,
}));
ipcMain.handle('runtime-open-logs', async () => {
	fs.mkdirSync(runtimeLogDirectory, { recursive: true, mode: 0o700 });
	const error = await shell.openPath(runtimeLogDirectory);
	if (error) throw new Error(error);
	desktopLogger.event('info', 'runtime', 'opened log directory');
});
ipcMain.handle('runtime-log', (_event, entry = {}) => {
	const value = entry && typeof entry === 'object' ? entry : {};
	const level = ['debug', 'info', 'warn', 'error'].includes(value.level) ? value.level : 'info';
	const feature = String(value.feature || 'renderer').replace(/[^a-zA-Z0-9._-]/g, '').slice(0, 80) || 'renderer';
	const message = String(value.message || '').slice(0, 16 * 1024);
	rendererLogger.event(level, feature, message);
	return true;
});
ipcMain.handle('wechat-service-status', () => currentWechatServiceStatus());
ipcMain.handle('open-wechat-login', async () => {
  const status = await currentWechatServiceStatus();
  if (!status.available || !status.login_url) throw new Error(status.message || '内置微信公众号服务不可用');
  return { ...status, login_url: `${status.login_url}?embedded=1&time=${Date.now()}` };
});
ipcMain.handle('browser-auth-status', (_event, profileId, source = 'xueqiu') => browserAuthStatus(source, profileId));
ipcMain.handle('open-review-source-login', (_event, source, profileId, homepageURL) => openReviewSourceLogin(source, profileId, homepageURL));
ipcMain.handle('open-xueqiu-login', (_event, profileId, homepageURL) => openReviewSourceLogin('xueqiu', profileId, homepageURL));
ipcMain.handle('review-browser-login-complete', (event) => {
  const window = BrowserWindow.fromWebContents(event.sender);
  const entry = [...reviewLoginWindows.values()].find((item) => item.window === window);
  if (!entry) throw new Error('登录窗口已失效');
  return entry.complete();
});
ipcMain.handle('app-update-status', () => updateManager?.getStatus() || {
  state: 'disabled', supported: false, currentVersion: app.getVersion(), progress: 0, message: '自动更新尚未初始化',
});
ipcMain.handle('app-update-check', () => updateManager.checkForUpdates());
ipcMain.handle('app-update-download', () => updateManager.downloadUpdate());
ipcMain.handle('app-update-install', () => updateManager.installUpdate());
ipcMain.handle('app-update-open-release', () => shell.openExternal(`https://github.com/jundizhou/easy-stock/releases/tag/v${updateManager?.getStatus().latestVersion || app.getVersion()}`));
ipcMain.handle('app-update-open-backups', async () => {
  const backupRoot = resolveBackupRoot(app.getPath('userData'));
  fs.mkdirSync(backupRoot, { recursive: true, mode: 0o700 });
  const error = await shell.openPath(backupRoot);
  if (error) throw new Error(error);
});

function browserAuthRoot() {
  return process.env.A_STOCK_BROWSER_STATE_DIR || path.join(app.getPath('userData'), 'browser-auth');
}

async function currentWechatServiceStatus() {
  if (!wechatServiceConfig) {
    return {
      available: false,
      configured: false,
      authenticated: false,
      state: wechatServiceError ? 'error' : 'starting',
      message: wechatServiceError ? `内置微信公众号服务启动失败：${wechatServiceError}` : '内置微信公众号服务正在启动',
    };
  }
  return readWechatServiceStatus(wechatServiceConfig.baseURL);
}

function browserAuthStatus(source, profileId) {
  const normalized = reviewBrowserSource(source);
  return readBrowserAuthStatus(statePathForProfile(browserAuthRoot(), profileId, normalized), normalized);
}

async function exportReviewStorageState(source, profileId, window) {
  const normalized = reviewBrowserSource(source);
  const partition = partitionForProfile(normalized, profileId);
  const partitionSession = session.fromPartition(partition);
  const cookies = (await partitionSession.cookies.get({})).filter((cookie) => isReviewSourceHost(cookie.domain, normalized));
  let origins = [];
  if (window && !window.isDestroyed() && !window.webContents.isDestroyed()) {
    try {
      const originState = await window.webContents.executeJavaScript(`(() => {
        const hostname = location.hostname.toLowerCase();
        const source = ${JSON.stringify(normalized)};
        const allowed = source === 'xueqiu'
          ? hostname === 'xueqiu.com' || hostname.endsWith('.xueqiu.com')
          : hostname === 'tgb.cn' || hostname.endsWith('.tgb.cn') || hostname === 'taoguba.com.cn' || hostname.endsWith('.taoguba.com.cn');
        if (!allowed) return null;
        const loginUserID = String(globalThis.loginUserID || '').trim();
        const loginUserName = String(globalThis.loginUserName || '').trim();
        const bodyText = String(document.body?.innerText || '');
        let storage = [];
        try {
          storage = Object.keys(localStorage).map((name) => ({ name, value: localStorage.getItem(name) || '' }));
        } catch {
          storage = [];
        }
        return {
          origin: location.origin,
          localStorage: storage,
          authenticated: source === 'taoguba' && ((loginUserID && loginUserID !== '0') || Boolean(loginUserName) || (/退出登录|退出账号/.test(bodyText) && !/登录\\s*\\/\\s*注册/.test(bodyText))),
        };
      })()`, true);
      if (originState?.origin) {
        const localStorage = (Array.isArray(originState.localStorage) ? originState.localStorage : [])
          .filter((item) => item?.name !== TAOGUBA_AUTH_MARKER && item?.name !== LEGACY_TAOGUBA_AUTH_MARKER);
        if (normalized === 'taoguba' && originState.authenticated) {
          localStorage.push({ name: TAOGUBA_AUTH_MARKER, value: '1' });
        }
        if (localStorage.length) origins = [{ origin: originState.origin, localStorage }];
      }
    } catch (error) {
		desktopLogger.event('warn', `${normalized}-login`, 'export localStorage failed', error);
    }
  }
  writeStorageState(statePathForProfile(browserAuthRoot(), profileId, normalized), {
    cookies: cookies.map(playwrightCookie),
    origins,
  });
  return browserAuthStatus(normalized, profileId);
}

function validReviewSourceURL(source, value) {
  const normalized = reviewBrowserSource(source);
  const fallback = normalized === 'taoguba' ? 'https://www.tgb.cn/' : 'https://xueqiu.com/';
  try {
    const parsed = new URL(String(value || fallback));
    if (parsed.protocol === 'https:' && isReviewSourceHost(parsed.hostname, normalized)) {
      return parsed.toString();
    }
  } catch {
    // Fall through to the known-safe login URL.
  }
  return fallback;
}

function openReviewSourceLogin(source, profileId, homepageURL) {
  const normalized = reviewBrowserSource(source);
  const label = normalized === 'taoguba' ? '淘股吧' : '雪球';
  const key = String(profileId || '').trim();
  if (!key) throw new Error(`${label}配置 ID 不能为空`);
  const windowKey = `${normalized}:${key}`;
  const existing = reviewLoginWindows.get(windowKey);
  if (existing && !existing.window.isDestroyed()) {
    existing.window.show();
    existing.window.focus();
    return existing.done;
  }

  const loginWindow = new BrowserWindow({
    width: 1120,
    height: 800,
    minWidth: 900,
    minHeight: 640,
    title: `${label}登录 · easy-stock`,
    backgroundColor: '#ffffff',
    autoHideMenuBar: true,
    webPreferences: {
      partition: partitionForProfile(normalized, key),
      preload: path.join(__dirname, 'review-login-preload.cjs'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  });

  let closing = false;
  let finalizing = null;
  let resolveDone;
  const done = new Promise((resolve) => { resolveDone = resolve; });
  const cookieStore = loginWindow.webContents.session.cookies;
  let exportTimer;
  const scheduleExport = () => {
    clearTimeout(exportTimer);
    exportTimer = setTimeout(() => {
		void exportReviewStorageState(normalized, key, loginWindow).catch((error) => desktopLogger.event('warn', `${normalized}-login`, 'save auth failed', error));
    }, 700);
  };
  const cookieListener = () => scheduleExport();
  const complete = (forceClose = false) => {
    if (finalizing) return finalizing;
    finalizing = exportReviewStorageState(normalized, key, loginWindow)
      .catch((error) => ({ configured: false, message: `保存${label}登录态失败：${error.message}` }))
      .then((status) => {
        if (forceClose || status.configured) {
          closing = true;
          resolveDone(status);
          loginWindow.destroy();
        }
        return status;
      })
      .finally(() => {
        if (!closing) finalizing = null;
      });
    return finalizing;
  };
  reviewLoginWindows.set(windowKey, { window: loginWindow, done, complete: () => complete(false) });
  cookieStore.on('changed', cookieListener);
  loginWindow.webContents.on('did-finish-load', scheduleExport);
  loginWindow.on('close', (event) => {
    if (closing) return;
    event.preventDefault();
    clearTimeout(exportTimer);
    void complete(true);
  });
  loginWindow.on('closed', () => {
    clearTimeout(exportTimer);
    cookieStore.removeListener('changed', cookieListener);
    reviewLoginWindows.delete(windowKey);
  });
  void loginWindow.loadURL(validReviewSourceURL(normalized, homepageURL)).catch((error) => {
		desktopLogger.event('warn', `${normalized}-login`, 'open login page failed', error);
  });
  return done;
}

function reviewBrowserSource(source) {
  const normalized = String(source || '').trim().toLowerCase();
  if (normalized !== 'xueqiu' && normalized !== 'taoguba') throw new Error('不支持的浏览器登录平台');
  return normalized;
}

function isReviewSourceHost(value, source) {
  const hostname = String(value || '').replace(/^\./, '').toLowerCase();
  if (source === 'xueqiu') return hostname === 'xueqiu.com' || hostname.endsWith('.xueqiu.com');
  return hostname === 'tgb.cn' || hostname.endsWith('.tgb.cn') || hostname === 'taoguba.com.cn' || hostname.endsWith('.taoguba.com.cn');
}

function safeURLPath(value) {
	try {
		return new URL(value).pathname;
	} catch {
		return '';
	}
}

app.whenReady().then(() => {
	desktopLogger.event('info', 'runtime', 'electron ready');
  const dockIcon = path.join(__dirname, 'assets', 'easy-stock.png');
  if (process.platform === 'darwin' && app.dock && fs.existsSync(dockIcon)) app.dock.setIcon(dockIcon);
  try {
    initializeUpdateManager();
  } catch (error) {
		desktopLogger.event('error', 'updater', 'initialization failed', error);
    updateManager = new UpdateManager({
      updater: autoUpdater,
      enabled: false,
      currentVersion: app.getVersion(),
      platform: process.platform,
      canInstallAutomatically: process.platform === 'win32',
			logger: featureLogger('updater'),
    });
  }
  createWindow().catch((error) => {
		desktopLogger.event('error', 'runtime', 'window startup failed', error);
    app.quit();
  });
});

app.on('before-quit', () => {
	desktopLogger.event('info', 'runtime', 'stopping');
  app.isQuitting = true;
  clearInterval(updateCheckTimer);
  if (xueqiuBrowserBridge) void xueqiuBrowserBridge.close();
  if (taogubaBrowserBridge) void taogubaBrowserBridge.close();
  if (backendProcess && !backendProcess.killed) backendProcess.kill();
  if (wechatServiceProcess && !wechatServiceProcess.killed) wechatServiceProcess.kill();
});

app.on('window-all-closed', () => {
  app.quit();
});
