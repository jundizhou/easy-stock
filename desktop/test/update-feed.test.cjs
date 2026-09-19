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

test('allows plain HTTP only for loopback feeds used by local end-to-end tests', () => {
  assert.equal(resolveUpdateFeedURL('http://127.0.0.1:8765/updates'), 'http://127.0.0.1:8765/updates');
  assert.equal(resolveUpdateFeedURL('http://localhost:8765/updates///'), 'http://localhost:8765/updates');
  assert.throws(() => resolveUpdateFeedURL('http://192.168.1.10:8765/updates'), /HTTPS/);
});
