const DEFAULT_UPDATE_FEED_URL = 'https://easy-stock-fs.oss-cn-beijing.aliyuncs.com/updates/desktop';

function resolveUpdateFeedURL(configuredURL = process.env.A_STOCK_UPDATE_FEED_URL) {
  const value = String(configuredURL || DEFAULT_UPDATE_FEED_URL).trim().replace(/\/+$/, '');
  const parsed = new URL(value);
  if (parsed.protocol !== 'https:') throw new Error('Desktop update feed must use HTTPS');
  return parsed.toString().replace(/\/$/, '');
}

module.exports = { DEFAULT_UPDATE_FEED_URL, resolveUpdateFeedURL };
