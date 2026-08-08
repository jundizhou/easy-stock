const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

const TAOGUBA_AUTH_MARKER = '__easy_stock_taoguba_login_verified';
const LEGACY_TAOGUBA_AUTH_MARKER = '__a_stock_ai_taoguba_login_verified';
const TAOGUBA_AUTH_MARKERS = new Set([TAOGUBA_AUTH_MARKER, LEGACY_TAOGUBA_AUTH_MARKER]);

function profileKey(profileId) {
  const value = String(profileId || '').trim();
  if (!value) throw new Error('profile_id_required');
  return crypto.createHash('sha256').update(value).digest('hex').slice(0, 32);
}

function normalizeSource(source = 'xueqiu') {
  const value = String(source || '').trim().toLowerCase();
  if (value !== 'xueqiu' && value !== 'taoguba') throw new Error('browser_auth_source_invalid');
  return value;
}

function partitionForProfile(source, profileId) {
  const normalized = normalizeSource(source);
  // Keep the pre-rename namespace so existing Electron login sessions remain usable.
  return `persist:a-stock-ai-${normalized}-${profileKey(profileId)}`;
}

function partitionForXueqiuProfile(profileId) {
  return partitionForProfile('xueqiu', profileId);
}

function partitionForTaogubaProfile(profileId) {
  return partitionForProfile('taoguba', profileId);
}

function statePathForProfile(root, profileId, source = 'xueqiu') {
  return path.join(root, normalizeSource(source), `${profileKey(profileId)}.json`);
}

function playwrightCookie(cookie) {
  const sameSite = String(cookie?.sameSite || '').toLowerCase();
  return {
    name: String(cookie?.name || ''),
    value: String(cookie?.value || ''),
    domain: String(cookie?.domain || ''),
    path: String(cookie?.path || '/'),
    expires: Number.isFinite(cookie?.expirationDate) ? cookie.expirationDate : -1,
    httpOnly: Boolean(cookie?.httpOnly),
    secure: Boolean(cookie?.secure),
    sameSite: sameSite === 'strict' ? 'Strict' : sameSite === 'lax' ? 'Lax' : 'None',
  };
}

function isXueqiuCookie(cookie) {
  const domain = String(cookie?.domain || '').replace(/^\./, '').toLowerCase();
  return domain === 'xueqiu.com' || domain.endsWith('.xueqiu.com');
}

function isTaogubaCookie(cookie) {
  const domain = String(cookie?.domain || '').replace(/^\./, '').toLowerCase();
  return domain === 'taoguba.com.cn' || domain.endsWith('.taoguba.com.cn')
    || domain === 'tgb.cn' || domain.endsWith('.tgb.cn');
}

function hasLoggedInXueqiuSession(storageState) {
  const cookies = Array.isArray(storageState?.cookies) ? storageState.cookies : [];
  const now = Date.now() / 1000;
  const values = new Map();
  for (const cookie of cookies) {
    if (!isXueqiuCookie(cookie)) continue;
    if (Number(cookie.expires) > 0 && Number(cookie.expires) <= now) continue;
    values.set(String(cookie.name || ''), String(cookie.value || ''));
  }
  return values.get('xq_is_login') === '1'
    || Boolean(values.get('u') && values.get('xq_id_token'));
}

function hasLoggedInTaogubaSession(storageState) {
  const origins = Array.isArray(storageState?.origins) ? storageState.origins : [];
  if (origins.some((origin) => Array.isArray(origin?.localStorage) && origin.localStorage.some((item) => TAOGUBA_AUTH_MARKERS.has(item?.name) && item?.value === '1'))) return true;
  const cookies = Array.isArray(storageState?.cookies) ? storageState.cookies : [];
  const now = Date.now() / 1000;
  return cookies.some((cookie) => {
    if (!isTaogubaCookie(cookie)) return false;
    if (Number(cookie.expires) > 0 && Number(cookie.expires) <= now) return false;
    const name = String(cookie.name || '').toLowerCase();
    const value = String(cookie.value || '');
    if (!value) return false;
    return /(?:^|_)(?:user(?:id|name)?|uid|login|auth|token)(?:$|_)/.test(name);
  });
}

function hasLoggedInBrowserSession(storageState, source = 'xueqiu') {
  return normalizeSource(source) === 'taoguba'
    ? hasLoggedInTaogubaSession(storageState)
    : hasLoggedInXueqiuSession(storageState);
}

function writeStorageState(filePath, storageState) {
  const directory = path.dirname(filePath);
  fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
  const temporaryPath = `${filePath}.${process.pid}.${Date.now()}.tmp`;
  fs.writeFileSync(temporaryPath, `${JSON.stringify(storageState, null, 2)}\n`, { mode: 0o600 });
  fs.renameSync(temporaryPath, filePath);
  fs.chmodSync(filePath, 0o600);
}

function readBrowserAuthStatus(filePath, source = 'xueqiu') {
  const normalized = normalizeSource(source);
  const label = normalized === 'taoguba' ? '淘股吧' : '雪球';
  const cookieFilter = normalized === 'taoguba' ? isTaogubaCookie : isXueqiuCookie;
  try {
    const stat = fs.statSync(filePath);
    const storageState = JSON.parse(fs.readFileSync(filePath, 'utf8'));
    const cookies = Array.isArray(storageState?.cookies) ? storageState.cookies.filter(cookieFilter) : [];
    const configured = hasLoggedInBrowserSession(storageState, normalized);
    return {
      configured,
      updated_at: stat.mtime.toISOString(),
      message: configured
        ? `${label}登录态已保存在本机，可供 Hermes 浏览器复用`
        : cookies.length
          ? `已保存浏览器会话，但尚未识别到有效的${label}登录状态`
          : `尚未保存${label}登录态`,
    };
  } catch (error) {
    if (error?.code !== 'ENOENT') console.warn(`read browser auth status failed: ${error.message}`);
    return { configured: false, message: `尚未保存${label}登录态` };
  }
}

module.exports = {
  LEGACY_TAOGUBA_AUTH_MARKER,
  TAOGUBA_AUTH_MARKER,
  hasLoggedInBrowserSession,
  hasLoggedInTaogubaSession,
  hasLoggedInXueqiuSession,
  normalizeSource,
  partitionForProfile,
  partitionForTaogubaProfile,
  partitionForXueqiuProfile,
  playwrightCookie,
  profileKey,
  readBrowserAuthStatus,
  statePathForProfile,
  writeStorageState,
};
