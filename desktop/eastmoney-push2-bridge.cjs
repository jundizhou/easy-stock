const crypto = require('node:crypto');
const http = require('node:http');

const DEFAULT_TIMEOUT_MS = 15_000;
const MAX_BODY_BYTES = 2 * 1024 * 1024;

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
        settle(reject, new Error('东方财富 push2 请求超时'));
      }, Math.max(1_000, Number(timeoutMs) || DEFAULT_TIMEOUT_MS));

      for (const [name, value] of Object.entries(headers || {})) {
        if (value) request.setHeader(name, value);
      }
      request.on('response', (response) => {
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
            settle(reject, new Error('东方财富 push2 响应过大'));
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
        response.on('error', (error) => settle(reject, error));
      });
      request.on('error', (error) => settle(reject, error));
      request.end();
    });
  };
}

function createEastmoneyPush2Bridge({ request, session, logger = console } = {}) {
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
    try {
      const startedAt = Date.now();
      const result = await forward({
        url: target,
        headers: {
          Accept: 'application/json, text/plain, */*',
          'Accept-Language': 'zh-CN,zh;q=0.9',
          Referer: 'https://quote.eastmoney.com/',
        },
        timeoutMs: DEFAULT_TIMEOUT_MS,
      });
      writeJSON(response, 200, {
        ok: true,
        data: {
          status: Number(result?.status) || 0,
          content_type: String(result?.contentType || ''),
          body: Buffer.isBuffer(result?.body) ? result.body.toString('utf8') : String(result?.body || ''),
          duration_ms: Date.now() - startedAt,
        },
      });
    } catch (error) {
      logger.warn?.(`eastmoney push2 bridge failed: ${error.message}`);
      writeJSON(response, 502, { ok: false, error: error.message || '东方财富 push2 请求失败' });
    }
  }

  async function start() {
    if (server) return { baseURL, token };
    server = http.createServer((request, response) => {
      handle(request, response).catch((error) => {
        logger.warn?.(`eastmoney push2 bridge failed: ${error.message}`);
        if (!response.headersSent) {
          writeJSON(response, 500, { ok: false, error: error.message || 'internal error' });
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
