const assert = require('node:assert/strict');
const test = require('node:test');

const { DEFAULT_UPDATE_FEED_URL, resolveUpdateFeedURL } = require('../update-feed.cjs');

test('uses the public OSS update feed by default', () => {
  assert.equal(resolveUpdateFeedURL(''), DEFAULT_UPDATE_FEED_URL);
});

test('normalizes and validates a configured HTTPS update feed', () => {
  assert.equal(resolveUpdateFeedURL('https://updates.example.com/desktop///'), 'https://updates.example.com/desktop');
  assert.throws(() => resolveUpdateFeedURL('http://updates.example.com/desktop'), /HTTPS/);
});
