const assert = require('node:assert/strict');
const test = require('node:test');
const { EventEmitter } = require('node:events');
const http = require('node:http');
const { buildBackendEnv, findFreePort, resolveBackendCommand, waitForHealth } = require('../backend-process.cjs');

test('health checks time out even when the server accepts but never responds', async () => {
  const server = http.createServer(() => {});
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  try {
    await assert.rejects(waitForHealth(`http://127.0.0.1:${server.address().port}`, 100));
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
});

test('backend process errors immediately stop health checks', async () => {
  const child = new EventEmitter();
  const health = waitForHealth('http://127.0.0.1:1', 15000, child);
  child.emit('error', new Error('spawn ENOENT'));
  await assert.rejects(health, /spawn ENOENT/);
  assert.equal(child.listenerCount('error'), 0);
  assert.equal(child.listenerCount('exit'), 0);
});

test('buildBackendEnv injects address and token', () => {
  const env = buildBackendEnv({ addr: '127.0.0.1:20001', token: 'abc123', baseEnv: { PATH: '/bin' } });

  assert.equal(env.PATH, '/bin');
  assert.equal(env.A_STOCK_ADDR, '127.0.0.1:20001');
  assert.equal(env.A_STOCK_TOKEN, 'abc123');
});

test('findFreePort allocates from the 20000+ desktop range', async () => {
  const port = await findFreePort();

  assert.ok(port >= 20000, `port ${port} should be >= 20000`);
  assert.ok(port <= 29999, `port ${port} should be <= 29999`);
});

test('resolveBackendCommand uses bundled binary when provided', () => {
  const command = resolveBackendCommand({
    backendBin: '/tmp/easy-stock-backend',
    backendDir: '/repo/backend',
    isPackaged: false,
  });

  assert.deepEqual(command, {
    command: '/tmp/easy-stock-backend',
    args: [],
    cwd: '/repo/backend',
  });
});

test('resolveBackendCommand falls back to go run in development', () => {
  const command = resolveBackendCommand({
    backendBin: '',
    backendDir: '/repo/backend',
    isPackaged: false,
  });

  assert.deepEqual(command, {
    command: 'go',
    args: ['run', './cmd/server'],
    cwd: '/repo/backend',
  });
});
