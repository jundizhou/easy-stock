const crypto = require('node:crypto');
const http = require('node:http');

const DEFAULT_TIMEOUT_MS = 15_000;
const MAX_BODY_BYTES = 2 * 1024 * 1024;
const QUOTE_PATHS = new Set([
  '/api/qt/stock/get', '/api/qt/stock/kline/get', '/api/qt/stock/fflow/kline/get',
  '/api/qt/stock/trends2/get', '/api/qt/clist/get', '/api/qt/ulist.np/get',
]);

// 东方财富 push2 系列（含编号镜像）的报价接口带浏览器指纹门禁：
// Go / curl / Node 这类非浏览器 TLS 客户端会在请求发出后被直接断连（EOF），
// 而同一时刻、同一出口 IP 的 Chromium 能正常拿到 JSON。
//
// 分时资金（fflow/kline）、分时价格（trends2）、报价列表（clist / ulist.np）
// 都在这条路径上，所以由桌面端把请求交给 Electron 的 Chromium 网络栈发出，
// 后端只和本机桥接通信。网页版没有该桥接，保持直连。
const PUSH2_HOST_PATTERN = /^(?:\d{1,3}\.)?push2(?:delay|his)?\.eastmoney\.com$/i;

// parseProxyTarget 只放行 push2 系列的 /api/ 报价接口，避免桥接变成任意转发器。
function parseProxyTarget(value) {
  let parsed;
  try {
    parsed = new URL(String(value || ''));
  } catch {
    return '';
  }
  if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') return '';
  if (!PUSH2_HOST_PATTERN.test(parsed.hostname)) return '';
  if (!parsed.pathname.startsWith('/api/')) return '';
  return parsed.toString();
}

// createChromiumRequester 用 Electron 的 net 模块发出请求。
//
// 走 Chromium 网络栈意味着 TLS/HTTP2 指纹与浏览器一致；同时 useSessionCookies
// 会带上该 session 里的 Cookie（例如会话桥接取得的 nid18），无需后端自行转发。
function createChromiumRequester(session) {
  return async function requestViaChromium({ url, headers, timeoutMs }) {
    const { net } = require('electron');
    return await new Promise((resolve, reject) => {
      let settled = false;
      let stage = 'request';
      let timer;
      const settle = (fn, value) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        fn(value);
      };
      const request = net.request({
        method: 'GET',
        url,
        session,
        useSessionCookies: true,
        redirect: 'follow',
      });
      timer = setTimeout(() => {
        try {
          request.abort();
        } catch {
          // 已结束的请求无法 abort，交由 settled 标记兜底。
        }
        settle(reject, Object.assign(new Error('东方财富 push2 请求超时'), { code: 'ETIMEDOUT', stage }));
      }, Math.max(1_000, Number(timeoutMs) || DEFAULT_TIMEOUT_MS));

      for (const [name, value] of Object.entries(headers || {})) {
        if (value) request.setHeader(name, value);
      }
      request.on('response', (response) => {
        stage = 'response';
        const chunks = [];
        let total = 0;
        response.on('data', (chunk) => {
          total += chunk.length;
          if (total > MAX_BODY_BYTES) {
            try {
              request.abort();
            } catch {
              // 同上。
            }
            settle(reject, Object.assign(new Error('东方财富 push2 响应过大'), { stage, code: 'EMSGSIZE' }));
            return;
          }
          chunks.push(chunk);
        });
        response.on('end', () => {
          settle(resolve, {
            status: response.statusCode,
            contentType: String(response.headers['content-type'] || ''),
            body: Buffer.concat(chunks),
          });
        });
        response.on('error', (error) => settle(reject, Object.assign(error, { stage })));
      });
      request.on('error', (error) => settle(reject, error));
      request.end();
    });
  };
}

function createEastmoneyPush2Bridge({ request, session, logger = console, now = Date.now } = {}) {
  const failures = new Map();
  const token = crypto.randomBytes(24).toString('hex');
  const forward = typeof request === 'function' ? request : createChromiumRequester(session);
  let server;
  let baseURL = '';

  async function handle(request_, response) {
    if (!authorized(request_, token)) {
      writeJSON(response, 401, { ok: false, error: 'unauthorized' });
      return;
    }
    const requestURL = new URL(request_.url || '/', 'http://127.0.0.1');
    if (request_.method === 'GET' && requestURL.pathname === '/health') {
      writeJSON(response, 200, { ok: true });
      return;
    }
    if (request_.method !== 'GET' || requestURL.pathname !== '/v1/eastmoney/push2') {
      writeJSON(response, 404, { ok: false, error: 'not_found' });
      return;
    }
    const target = parseProxyTarget(requestURL.searchParams.get('url'));
    if (!target) {
      writeJSON(response, 400, { ok: false, error: 'unsupported push2 url' });
      return;
    }
    const { hostname: host, pathname } = new URL(target);
    const route = QUOTE_PATHS.has(pathname) ? pathname : '/api/other';
    const key = host + route;
    const previous = failures.get(key);
    if (previous && (previous.retryAt > now() || previous.probing)) {
      writeJSON(response, 503, { ok: false, error: 'push2_cooldown' });
      return;
    }
    if (previous) previous.probing = true;
    let stage = 'request';
    try {
      const startedAt = now();
      const result = await forward({
        url: target,
        headers: {
          Accept: 'application/json, text/plain, */*',
          'Accept-Language': 'zh-CN,zh;q=0.9',
          Referer: 'https://quote.eastmoney.com/',
        },
        timeoutMs: DEFAULT_TIMEOUT_MS,
      });
      stage = 'response';
      const body = Buffer.isBuffer(result?.body) ? result.body.toString('utf8') : String(result?.body || '');
      if (result?.status === 200 && !body.trim()) {
        throw Object.assign(new Error('empty response'), { code: 'EMPTY_BODY' });
      }
      failures.delete(key);
      writeJSON(response, 200, {
        ok: true,
        data: {
          status: Number(result?.status) || 0,
          content_type: String(result?.contentType || ''),
          body,
          duration_ms: now() - startedAt,
        },
      });
    } catch (error) {
      if (error?.stage === 'response') stage = 'response';
      const code = /^E[A-Z0-9_]{1,50}$/.test(error?.code) ? error.code
        : /\bnet::(ERR_[A-Z0-9_]+)/.exec(String(error?.message))?.[1] || 'NETWORK_ERROR';
      // Concurrent failures from the same outage share a single warning and retry window.
      if (failures.get(key) === previous) {
        const delay = Math.min((previous?.delay || 5_000) * 2, 60_000);
        failures.set(key, { retryAt: now() + delay, delay, probing: false });
        logger.warn?.(`eastmoney push2 bridge stage=${stage} host=${host} route=${route} code=${code} retry_ms=${delay}`);
      }
      writeJSON(response, 502, { ok: false, error: code });
    }
  }

  async function start() {
    if (server) return { baseURL, token };
    server = http.createServer((request, response) => {
      handle(request, response).catch(() => {
        logger.warn?.('eastmoney push2 bridge stage=handler code=INTERNAL_ERROR');
        if (!response.headersSent) {
          writeJSON(response, 500, { ok: false, error: 'internal error' });
        } else {
          response.destroy();
        }
      });
    });
    await new Promise((resolve, reject) => {
      server.once('error', reject);
      server.listen(0, '127.0.0.1', resolve);
    });
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('东方财富 push2 桥接端口不可用');
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
    get baseURL() {
      return baseURL;
    },
    token,
  };
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

module.exports = {
  createChromiumRequester,
  createEastmoneyPush2Bridge,
  parseProxyTarget,
};
