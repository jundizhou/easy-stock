const fs = require('node:fs');
const path = require('node:path');
const { spawn } = require('node:child_process');

const WECHAT_RUNTIME_MANIFEST = 'easy-stock-wechat-runtime.json';
const PERSISTED_ENTRIES = new Set(['.env', 'data']);

function resolveWechatPython(runtimeRoot, platform = process.platform) {
  return platform === 'win32'
    ? path.join(runtimeRoot, 'python', 'python.exe')
    : path.join(runtimeRoot, 'venv', 'bin', 'python');
}

function buildWechatEnv({ baseURL, port, baseEnv = process.env }) {
  return {
    ...baseEnv,
    HOST: '127.0.0.1',
    PORT: String(port),
    SITE_URL: baseURL,
    ENABLE_MCP: '0',
    RSS_FETCH_FULL_CONTENT: 'false',
    SKIP_BACKGROUND_TASKS: '1',
    DEBUG: 'false',
    PYTHONNOUSERSITE: '1',
    PYTHONDONTWRITEBYTECODE: '1',
    NO_PROXY: appendNoProxy(baseEnv.NO_PROXY || baseEnv.no_proxy || ''),
  };
}

function appendNoProxy(value) {
  const entries = String(value || '').split(',').map((item) => item.trim()).filter(Boolean);
  for (const host of ['127.0.0.1', 'localhost']) {
    if (!entries.includes(host)) entries.push(host);
  }
  return entries.join(',');
}

function syncWechatServiceSource({ sourceDir, workDir }) {
  const sourceManifest = readJSON(path.join(sourceDir, WECHAT_RUNTIME_MANIFEST));
  if (!sourceManifest?.revision) throw new Error('内置微信公众号服务缺少版本清单');
  if (!fs.existsSync(path.join(sourceDir, 'app.py'))) throw new Error('内置微信公众号服务源码不完整');

  const installedManifestPath = path.join(workDir, WECHAT_RUNTIME_MANIFEST);
  const installedManifest = readJSON(installedManifestPath);
  if (installedManifest?.revision === sourceManifest.revision && fs.existsSync(path.join(workDir, 'app.py'))) {
    ensureWechatEnv(workDir);
    return { workDir, revision: sourceManifest.revision, updated: false };
  }

  fs.mkdirSync(workDir, { recursive: true, mode: 0o700 });
  for (const entry of fs.readdirSync(workDir)) {
    if (PERSISTED_ENTRIES.has(entry)) continue;
    fs.rmSync(path.join(workDir, entry), { recursive: true, force: true });
  }
  copyTree(sourceDir, workDir, (relativePath) => {
    const rootEntry = relativePath.split(path.sep)[0];
    return !PERSISTED_ENTRIES.has(rootEntry);
  });
  ensureWechatEnv(workDir);
  return { workDir, revision: sourceManifest.revision, updated: true };
}

function copyTree(sourceDir, targetDir, include) {
  for (const entry of fs.readdirSync(sourceDir, { withFileTypes: true })) {
    const relativePath = entry.name;
    copyEntry(sourceDir, targetDir, relativePath, include);
  }
}

function copyEntry(sourceRoot, targetRoot, relativePath, include) {
  if (!include(relativePath)) return;
  const sourcePath = path.join(sourceRoot, relativePath);
  const targetPath = path.join(targetRoot, relativePath);
  const stat = fs.lstatSync(sourcePath);
  if (stat.isDirectory()) {
    fs.mkdirSync(targetPath, { recursive: true });
    for (const entry of fs.readdirSync(sourcePath)) {
      copyEntry(sourceRoot, targetRoot, path.join(relativePath, entry), include);
    }
    return;
  }
  fs.mkdirSync(path.dirname(targetPath), { recursive: true });
  fs.copyFileSync(sourcePath, targetPath);
  if (stat.mode & 0o111) fs.chmodSync(targetPath, stat.mode & 0o777);
}

function ensureWechatEnv(workDir) {
  const envPath = path.join(workDir, '.env');
  if (fs.existsSync(envPath)) return;
  fs.writeFileSync(envPath, [
    '# Managed by easy-stock. Login credentials are appended by the bundled service.',
    'ENABLE_MCP=0',
    'RSS_FETCH_FULL_CONTENT=false',
    'SKIP_BACKGROUND_TASKS=1',
    '',
  ].join('\n'), { mode: 0o600 });
}

function startWechatService({ python, workDir, port, baseURL, spawnImpl = spawn }) {
  if (!fs.existsSync(python)) throw new Error(`内置 Python 运行时不存在：${python}`);
  return spawnImpl(python, ['-m', 'uvicorn', 'app:app', '--host', '127.0.0.1', '--port', String(port), '--log-level', 'warning'], {
    cwd: workDir,
    env: buildWechatEnv({ baseURL, port }),
    stdio: ['ignore', 'pipe', 'pipe'],
  });
}

async function waitForWechatHealth(baseURL, timeoutMS = 30000, fetchImpl = fetch) {
  const startedAt = Date.now();
  let lastError;
  while (Date.now() - startedAt < timeoutMS) {
    try {
      const response = await fetchImpl(new URL('/api/health', baseURL));
      if (response.ok) return;
      lastError = new Error(`微信公众号服务健康检查返回 HTTP ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 300));
  }
  throw lastError || new Error('微信公众号服务启动超时');
}

async function readWechatServiceStatus(baseURL, fetchImpl = fetch, now = Date.now()) {
  if (!baseURL) return unavailableStatus('内置微信公众号服务尚未启动');
  try {
    const health = await fetchImpl(new URL('/api/health', baseURL));
    if (!health.ok) return unavailableStatus(`内置微信公众号服务异常（HTTP ${health.status}）`);
    const response = await fetchImpl(new URL('/api/login/info', baseURL));
    if (!response.ok) return unavailableStatus(`无法读取微信公众号登录状态（HTTP ${response.status}）`);
    const payload = await response.json();
    return statusFromLoginInfo(payload, now, baseURL);
  } catch (error) {
    return unavailableStatus(`内置微信公众号服务不可用：${error.message || error}`);
  }
}

function statusFromLoginInfo(payload, now = Date.now(), baseURL = '') {
  if (!payload?.success || !payload?.data) {
    return {
      available: true,
      configured: false,
      authenticated: false,
      state: 'not_logged_in',
      message: '服务已自动配置；扫码登录后可解析已知微信公众号文章链接',
      login_url: baseURL ? new URL('/login.html', baseURL).toString() : undefined,
    };
  }
  const expiresAtMS = Number(payload.data.expire_time || 0);
  const expired = expiresAtMS > 0 && expiresAtMS <= now;
  const account = String(payload.data.nickname || '').trim() || '微信公众号';
  return {
    available: true,
    configured: true,
    authenticated: !expired,
    state: expired ? 'expired' : 'authenticated',
    account,
    fakeid: String(payload.data.fakeid || '').trim() || undefined,
    expires_at: expiresAtMS > 0 ? new Date(expiresAtMS).toISOString() : undefined,
    message: expired ? `${account} 登录已过期，请重新扫码` : `${account} 已登录，可解析已知文章链接；自动历史文章列表暂不可用`,
    login_url: baseURL ? new URL('/login.html', baseURL).toString() : undefined,
  };
}

function unavailableStatus(message) {
  return {
    available: false,
    configured: false,
    authenticated: false,
    state: 'error',
    message,
  };
}

function readJSON(filePath) {
  try {
    return JSON.parse(fs.readFileSync(filePath, 'utf8'));
  } catch {
    return null;
  }
}

module.exports = {
  WECHAT_RUNTIME_MANIFEST,
  buildWechatEnv,
  readWechatServiceStatus,
  resolveWechatPython,
  startWechatService,
  statusFromLoginInfo,
  syncWechatServiceSource,
  waitForWechatHealth,
};
