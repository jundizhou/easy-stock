const crypto = require('node:crypto');
const http = require('node:http');

const MAX_REQUEST_BYTES = 32 << 10;
const DEFAULT_PAGE_TIMEOUT_MS = 30_000;

function createXueqiuBrowserBridge({ BrowserWindow, partitionForProfile, collector, logger = console }) {
  if (typeof BrowserWindow !== 'function' && typeof collector !== 'function') {
    throw new Error('BrowserWindow is required');
  }
  if (typeof partitionForProfile !== 'function' && typeof collector !== 'function') {
    throw new Error('partitionForProfile is required');
  }

  const token = crypto.randomBytes(24).toString('hex');
  const collect = collector || ((input) => collectXueqiuWithElectron({
    BrowserWindow,
    partitionForProfile,
    ...input,
  }));
  let server;
  let baseURL = '';

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
        if (request.method !== 'POST' || requestURL.pathname !== '/v1/xueqiu/collect') {
          writeJSON(response, 404, { ok: false, error: 'not_found' });
          return;
        }
        const body = await readJSON(request);
        const profileId = String(body.profile_id || '').trim();
        const homepageURL = validXueqiuURL(body.homepage_url);
        const limit = Math.max(1, Math.min(Number(body.limit) || 5, 5));
        if (!profileId) throw new Error('雪球配置 ID 不能为空');
        const data = await collect({ profileId, homepageURL, limit });
        writeJSON(response, 200, { ok: true, data });
      } catch (error) {
        logger.warn?.(`xueqiu browser bridge failed: ${error.message}`);
        writeJSON(response, 502, { ok: false, error: error.message || '雪球浏览器采集失败' });
      }
    });
    await new Promise((resolve, reject) => {
      server.once('error', reject);
      server.listen(0, '127.0.0.1', resolve);
    });
    const address = server.address();
    if (!address || typeof address === 'string') throw new Error('雪球浏览器桥接端口不可用');
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

async function collectXueqiuWithElectron({ BrowserWindow, partitionForProfile, profileId, homepageURL, limit = 5 }) {
  const window = new BrowserWindow({
    width: 1180,
    height: 820,
    show: false,
    backgroundColor: '#ffffff',
    webPreferences: {
      partition: partitionForProfile(profileId),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      backgroundThrottling: false,
    },
  });
  window.webContents.setWindowOpenHandler(() => ({ action: 'deny' }));
  try {
    await loadPage(window, homepageURL);
    const homepage = await window.webContents.executeJavaScript(`(${homepageExtraction.toString()})()`, true);
    assertReadablePage(homepage);
    const articles = [];
    const seen = new Set();
    for (const candidate of homepage.candidates || []) {
      if (articles.length >= limit) break;
      const articleURL = validXueqiuArticleURL(candidate.url);
      if (!articleURL || seen.has(articleURL)) continue;
      seen.add(articleURL);
      let extracted;
      try {
        await loadPage(window, articleURL);
        extracted = await window.webContents.executeJavaScript(`(${articleExtraction.toString()})()`, true);
        assertReadablePage(extracted);
      } catch (error) {
        if (String(error.message || '').includes('滑动验证')) throw error;
        extracted = null;
      }
      const contentText = cleanText(extracted?.content_text || candidate.content_text || '', 50_000);
      const title = cleanText(extracted?.title || candidate.title || firstContentLine(contentText), 300);
      if (!title || contentText.length < 20) continue;
      articles.push({
        title,
        original_url: articleURL,
        content_text: contentText,
        published_at: cleanText(extracted?.published_at || candidate.published_at || '', 120),
      });
      await delay(450);
    }
    if (!articles.length) throw new Error('内置浏览器未发现可读取的雪球文章');
    return {
      author_name: cleanText(homepage.author_name || '', 120),
      external_id: cleanText(homepage.external_id || lastPathPart(homepageURL), 80),
      articles,
      error: '',
    };
  } finally {
    if (!window.isDestroyed()) window.destroy();
  }
}

async function loadPage(window, pageURL) {
  await Promise.race([
    window.loadURL(pageURL),
    new Promise((_, reject) => setTimeout(() => reject(new Error('雪球页面加载超时')), DEFAULT_PAGE_TIMEOUT_MS)),
  ]);
  await delay(1_800);
}

function homepageExtraction() {
  const clean = (value, limit = 50000) => String(value || '').replace(/\u00a0/g, ' ').replace(/[ \t]+\n/g, '\n').replace(/\n{3,}/g, '\n\n').trim().slice(0, limit);
  const bodyText = clean(document.body?.innerText || '', 100000);
  const title = clean(document.title, 300);
  const verification = /滑动验证|访问验证|安全验证|完成验证/.test(`${title}\n${bodyText}`) || location.search.includes('alichlgref=');
  const loginRequired = /登录后查看|立即登录|登录\s*\/\s*注册/.test(bodyText);
  const pathnameParts = location.pathname.split('/').filter(Boolean);
  const externalId = pathnameParts[0] === 'u' ? pathnameParts[1] || '' : pathnameParts[0] || '';
  const authorSelectors = ['.user-name', '.profile__name', '.profile-name', '[class*="userName"]', '[class*="user-name"]', 'h1'];
  let authorName = '';
  for (const selector of authorSelectors) {
    const value = clean(document.querySelector(selector)?.textContent || '', 120);
    if (value && value.length <= 80) { authorName = value; break; }
  }
  if (!authorName) {
    authorName = clean(document.querySelector('meta[property="og:title"]')?.content || '', 120).replace(/\s*[-_|].*雪球.*$/i, '');
  }
  const results = [];
  const seen = new Set();
  for (const anchor of document.querySelectorAll('a[href]')) {
    let target;
    try { target = new URL(anchor.href, location.href); } catch { continue; }
    if (target.protocol !== 'https:' || !(target.hostname === 'xueqiu.com' || target.hostname.endsWith('.xueqiu.com'))) continue;
    if (!/^\/\d+\/\d+\/?$/.test(target.pathname)) continue;
    const canonical = `https://xueqiu.com${target.pathname.replace(/\/$/, '')}`;
    if (seen.has(canonical)) continue;
    const card = anchor.closest('article, [class*="timeline"], [class*="status"], [class*="feed"], [class*="content"], li') || anchor.parentElement;
    const cardText = clean(card?.innerText || anchor.innerText || '', 50000);
    if (cardText.length < 20) continue;
    const heading = clean(card?.querySelector?.('h1,h2,h3,h4,[class*="title"]')?.textContent || anchor.textContent || '', 300);
    const timeNode = card?.querySelector?.('time,[datetime],[class*="time"],[class*="date"]');
    const publishedAt = clean(timeNode?.getAttribute?.('datetime') || timeNode?.textContent || '', 120);
    results.push({ url: canonical, title: heading, content_text: cardText, published_at: publishedAt });
    seen.add(canonical);
    if (results.length >= 12) break;
  }
  return { url: location.href, title, verification, login_required: loginRequired, author_name: authorName, external_id: externalId, candidates: results };
}

function articleExtraction() {
  const clean = (value, limit = 50000) => String(value || '').replace(/\u00a0/g, ' ').replace(/[ \t]+\n/g, '\n').replace(/\n{3,}/g, '\n\n').trim().slice(0, limit);
  const bodyText = clean(document.body?.innerText || '', 100000);
  const pageTitle = clean(document.title, 300);
  const verification = /滑动验证|访问验证|安全验证|完成验证/.test(`${pageTitle}\n${bodyText}`) || location.search.includes('alichlgref=');
  const loginRequired = /登录后查看|立即登录|登录\s*\/\s*注册/.test(bodyText);
  const roots = ['.article__bd', '.article-content', '.article_content', '[class*="article__bd"]', '[class*="article-content"]', '.status-content', '[class*="status-content"]', 'article', 'main'];
  let contentText = '';
  for (const selector of roots) {
    for (const node of document.querySelectorAll(selector)) {
      const text = clean(node.innerText || node.textContent || '', 50000);
      if (text.length > contentText.length) contentText = text;
    }
    if (contentText.length >= 120) break;
  }
  if (!contentText) contentText = bodyText;
  const heading = clean(document.querySelector('h1,[class*="article-title"],[class*="article__title"]')?.textContent || '', 300);
  const metaTitle = clean(document.querySelector('meta[property="og:title"]')?.content || '', 300);
  const title = heading || metaTitle || pageTitle.replace(/\s*[-_|].*雪球.*$/i, '');
  const timeNode = document.querySelector('time,[datetime],[class*="publish-time"],[class*="article__time"]');
  const publishedAt = clean(timeNode?.getAttribute?.('datetime') || timeNode?.textContent || document.querySelector('meta[property="article:published_time"]')?.content || '', 120);
  return { url: location.href, title: clean(title, 300), content_text: clean(contentText, 50000), published_at: publishedAt, verification, login_required: loginRequired };
}

function assertReadablePage(page) {
  if (page?.verification) throw new Error('雪球仍要求滑动验证；请在内置登录窗口完成验证后重试');
  if (page?.login_required) throw new Error('雪球浏览器会话已失去登录状态，请重新登录');
}

function validXueqiuURL(value) {
  const parsed = new URL(String(value || ''));
  if (parsed.protocol !== 'https:' || (parsed.hostname !== 'xueqiu.com' && !parsed.hostname.endsWith('.xueqiu.com'))) {
    throw new Error('只允许访问雪球 HTTPS 页面');
  }
  parsed.hash = '';
  return parsed.toString();
}

function validXueqiuArticleURL(value) {
  try {
    const parsed = new URL(String(value || ''));
    if (parsed.protocol !== 'https:' || (parsed.hostname !== 'xueqiu.com' && !parsed.hostname.endsWith('.xueqiu.com'))) return '';
    if (!/^\/\d+\/\d+\/?$/.test(parsed.pathname)) return '';
    return `https://xueqiu.com${parsed.pathname.replace(/\/$/, '')}`;
  } catch {
    return '';
  }
}

function authorized(request, token) {
  const supplied = String(request.headers['x-a-stock-browser-token'] || '');
  if (supplied.length !== token.length) return false;
  return crypto.timingSafeEqual(Buffer.from(supplied), Buffer.from(token));
}

function readJSON(request) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    request.on('data', (chunk) => {
      size += chunk.length;
      if (size > MAX_REQUEST_BYTES) {
        reject(new Error('浏览器桥接请求过大'));
        request.destroy();
        return;
      }
      chunks.push(chunk);
    });
    request.on('end', () => {
      try {
        resolve(JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}'));
      } catch {
        reject(new Error('浏览器桥接请求格式无效'));
      }
    });
    request.on('error', reject);
  });
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

function cleanText(value, limit) {
  return String(value || '').replace(/\u00a0/g, ' ').replace(/[ \t]+\n/g, '\n').replace(/\n{3,}/g, '\n\n').trim().slice(0, limit);
}

function firstContentLine(value) {
  return String(value || '').split('\n').map((line) => line.trim()).find(Boolean) || '';
}

function lastPathPart(value) {
  try {
    return new URL(value).pathname.split('/').filter(Boolean).pop() || '';
  } catch {
    return '';
  }
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

module.exports = {
  collectXueqiuWithElectron,
  createXueqiuBrowserBridge,
  validXueqiuArticleURL,
  validXueqiuURL,
};
