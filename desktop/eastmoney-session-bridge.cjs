const crypto = require('node:crypto');
const http = require('node:http');

const DEFAULT_WARM_TIMEOUT_MS = 20_000;
const COOKIE_POLL_INTERVAL_MS = 300;
// 拿不到过期时间时按固定 TTL 复用，避免每次请求都重新加载页面。
const FALLBACK_CACHE_TTL_MS = 10 * 60 * 1000;
const EXPIRY_GUARD_MS = 30 * 1000;

// 东方财富 push2 系的资金流分时接口（api/qt/stock/fflow/kline/get）有会话门禁：
// 不带 nid18 时上游会直接断开连接（Go EOF / curl 56 "server closed abruptly"），
// 带上任意一份浏览器下发的 nid18 即可正常返回逐分钟累计净流入。
//
// nid18 由东财页面里的指纹 SDK（ewtsdk）下发，无法在后端伪造或离线推导，
// 因此这里用隐藏窗口在真实浏览器环境里取一份，再把 Cookie 转交给后端使用。
const SESSION_PAGE_URL = 'https://data.eastmoney.com/bkzj/gn.html';
const SESSION_COOKIE_NAME = 'nid18';
const EASTMONEY_HOST = 'eastmoney.com';

function createEastmoneySessionBridge({ BrowserWindow, acquire, logger = console }) {
  if (typeof acquire !== 'function' && typeof BrowserWindow !== 'function') {
    throw new Error('BrowserWindow is required');
  }

  const token = crypto.randomBytes(24).toString('hex');
  const loadSession = acquire || ((input) => acquireEastmoneySession({ BrowserWindow, ...input }));
  let server;
  let baseURL = '';
  let cached;
  let inflight;

  function cachedFresh() {
    return Boolean(cached) && (!cached.expiresAt || cached.expiresAt - EXPIRY_GUARD_MS > Date.now());
  }

  async function session(refresh) {
    if (!refresh && cachedFresh()) return cached;
    if (inflight) return inflight;
    inflight = (async () => {
      try {
        const acquired = await loadSession({ refresh, timeoutMs: DEFAULT_WARM_TIMEOUT_MS });
        const value = String(acquired?.value || '').trim();
        if (!value) throw new Error('东方财富会话未返回有效的 nid18');
        const expiresAt = Date.parse(String(acquired.expires_at || '')) || 0;
        cached = {
          cookie: `${String(acquired.name || SESSION_COOKIE_NAME).trim() || SESSION_COOKIE_NAME}=${value}`,
          expiresAt: expiresAt || Date.now() + FALLBACK_CACHE_TTL_MS,
        };
        return cached;
      } finally {
        inflight = undefined;
      }
    })();
    try {
      return await inflight;
    } catch (error) {
      // 刷新失败时保留上一份会话，后端仍有机会继续用旧 Cookie 取数。
      if (cached) return cached;
      throw error;
    }
  }

  async function start() {
    if (server) return { baseURL, token };
    server = http.createServer(async (request, response) => {
      try {
        if (!authorized(request, token)) {
          writeJSON(response, 401, { ok: false, error: 'unauthorized' });
          return;
        }
        const requestURL = new URL(request.url || '/', 'http://127.0.0.1');
        if (request.method === 'GET' && requestURL.pathname === '/health') {
          writeJSON(response, 200, { ok: true });
          return;
        }
        if (request.method !== 'GET' || requestURL.pathname !== '/v1/eastmoney/session') {
          writeJSON(response, 404, { ok: false, error: 'not_found' });
          return;
        }
        const refresh = ['1', 'true'].includes(String(requestURL.searchParams.get('refresh') || '').toLowerCase());
        const value = await session(refresh);
        writeJSON(response, 200, {
          ok: true,
          data: {
            cookie: value.cookie,
            expires_at: value.expiresAt ? new Date(value.expiresAt).toISOString() : '',
          },
        });
      } catch (error) {
        logger.warn?.(`eastmoney session bridge failed: ${error.message}`);
        writeJSON(response, 502, { ok: false, error: error.message || '东方财富会话获取失败' });
      }
    });
    await new Promise((resolve, reject) => {
      server.once('error', reject);
      server.listen(0, '127.0.0.1', resolve);
    });
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('东方财富会话桥接端口不可用');
    baseURL = `http://127.0.0.1:${address.port}`;
    return { baseURL, token };
  }

  async function close() {
    const active = server;
    server = undefined;
    baseURL = '';
    if (!active) return;
    await new Promise((resolve) => active.close(() => resolve()));
  }

  return {
    start,
    close,
    get baseURL() { return baseURL; },
    token,
  };
}

// acquireEastmoneySession 在隐藏窗口里加载东财板块页，等指纹 SDK 下发 nid18 后读出来。
//
// refresh 为 true 时跳过 Cookie 缓存快路径，把页面完整走一遍再重读：
// 上游按突发流量拒绝 fflow 请求后，只有真实浏览器重新访问页面才会恢复取数，
// 页面加载被打断时（窗口提前销毁）恢复不了，所以这一路径必须等页面加载结束。
async function acquireEastmoneySession({ BrowserWindow, refresh = false, timeoutMs = DEFAULT_WARM_TIMEOUT_MS }) {
  if (typeof BrowserWindow !== 'function') throw new Error('BrowserWindow is required');
  const window = new BrowserWindow({
    width: 1100,
    height: 760,
    show: false,
    backgroundColor: '#ffffff',
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      backgroundThrottling: false,
    },
  });
  window.webContents.setWindowOpenHandler(() => ({ action: 'deny' }));
  try {
    const cookieStore = window.webContents.session.cookies;
    if (!refresh) {
      const existing = readEastmoneySessionCookie(await cookieStore.get({ name: SESSION_COOKIE_NAME }));
      if (existing) return existing;
    }
    const deadline = Date.now() + Math.max(1_000, Number(timeoutMs) || DEFAULT_WARM_TIMEOUT_MS);
    if (refresh) {
      // 刷新路径等页面加载完成；加载失败也继续，至少还能复用会话里已有的 Cookie。
      const loaded = window.loadURL(SESSION_PAGE_URL).catch(() => {});
      await Promise.race([loaded, delay(Math.max(1_000, deadline - Date.now()))]);
    } else {
      // 不等待页面加载完成：只要 SDK 把 Cookie 写进会话即可，页面渲染与业务数据无关。
      window.loadURL(SESSION_PAGE_URL).catch(() => {});
    }
    while (Date.now() < deadline) {
      const cookie = readEastmoneySessionCookie(await cookieStore.get({ name: SESSION_COOKIE_NAME }));
      if (cookie) return cookie;
      await delay(COOKIE_POLL_INTERVAL_MS);
    }
    throw new Error('等待东方财富会话 Cookie（nid18）超时，请确认本机可访问 data.eastmoney.com');
  } finally {
    if (!window.isDestroyed()) window.destroy();
  }
}

function readEastmoneySessionCookie(cookies, now = Date.now()) {
  for (const cookie of Array.isArray(cookies) ? cookies : []) {
    const domain = String(cookie?.domain || '').replace(/^\./, '').toLowerCase();
    if (domain !== EASTMONEY_HOST && !domain.endsWith(`.${EASTMONEY_HOST}`)) continue;
    const value = String(cookie?.value || '').trim();
    if (!value) continue;
    const expiresAt = Number(cookie?.expirationDate) > 0 ? Number(cookie.expirationDate) * 1000 : 0;
    if (expiresAt && expiresAt <= now) continue;
    return {
      name: String(cookie.name || SESSION_COOKIE_NAME).trim() || SESSION_COOKIE_NAME,
      value,
      expires_at: expiresAt ? new Date(expiresAt).toISOString() : '',
    };
  }
  return null;
}

function isEastmoneyCookie(cookie) {
  const domain = String(cookie?.domain || '').replace(/^\./, '').toLowerCase();
  return domain === EASTMONEY_HOST || domain.endsWith(`.${EASTMONEY_HOST}`);
}

function authorized(request, token) {
  const supplied = String(request.headers['x-a-stock-browser-token'] || '');
  if (supplied.length !== token.length) return false;
  return crypto.timingSafeEqual(Buffer.from(supplied), Buffer.from(token));
}

function writeJSON(response, status, value) {
  const body = Buffer.from(JSON.stringify(value));
  response.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': body.length,
    'Cache-Control': 'no-store',
  });
  response.end(body);
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

module.exports = {
  acquireEastmoneySession,
  createEastmoneySessionBridge,
  isEastmoneyCookie,
  readEastmoneySessionCookie,
};
