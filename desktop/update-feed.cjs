const DEFAULT_UPDATE_FEED_URL = 'https://easy-stock-fs.oss-cn-beijing.aliyuncs.com/updates/desktop';

const LOOPBACK_HOSTNAMES = new Set(['localhost', '127.0.0.1', '[::1]', '::1']);

function resolveUpdateFeedURL(configuredURL = process.env.A_STOCK_UPDATE_FEED_URL) {
  const value = String(configuredURL || DEFAULT_UPDATE_FEED_URL).trim().replace(/\/+$/, '');
  const parsed = new URL(value);
  const isLoopback = LOOPBACK_HOSTNAMES.has(parsed.hostname);
  if (parsed.protocol !== 'https:' && !(parsed.protocol === 'http:' && isLoopback)) {
    throw new Error('Desktop update feed must use HTTPS');
  }
  return parsed.toString().replace(/\/$/, '');
}

module.exports = { DEFAULT_UPDATE_FEED_URL, resolveUpdateFeedURL };
