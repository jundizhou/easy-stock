const assert = require('node:assert/strict');
const test = require('node:test');
const { EventEmitter } = require('node:events');
const http = require('node:http');
const { spawn } = require('node:child_process');
const { once } = require('node:events');
const { buildBackendEnv, findFreePort, resolveBackendCommand, terminateChild, waitForHealth } = require('../backend-process.cjs');

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

function fakeProcess() {
  return Object.assign(new EventEmitter(), {
    pid: 12345, exitCode: null, signalCode: null, killed: false,
    kill() { assert.fail('Windows must not terminate the parent before taskkill walks the tree'); },
  });
}

test('Windows shutdown waits for both the process tree command and the parent exit', async () => {
  const child = fakeProcess();
  const killer = new EventEmitter();
  let resolved = false;
  const stop = terminateChild(child, 1000, { platform: 'win32', spawnImpl(command, args, options) {
    assert.match(command, /System32\\taskkill\.exe$/i);
    assert.deepEqual(args, ['/PID', '12345', '/T', '/F']);
    assert.equal(options.windowsHide, true);
    return killer;
  } }).then(() => { resolved = true; });
  child.emit('exit', 0);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(resolved, false, 'parent exit alone cannot confirm descendant cleanup');
  killer.emit('close', 0);
  await stop;
  assert.equal(child.listenerCount('exit'), 0);
});

test('a previously signalled process is still stopped until it actually exits', async () => {
  const child = fakeProcess();
  child.killed = true;
  const killer = new EventEmitter();
  const stop = terminateChild(child, 1000, { platform: 'win32', spawnImpl: () => killer });
  killer.emit('close', 0);
  child.emit('exit', 0);
  await stop;
});

test('Windows shutdown reports taskkill launch and access failures', async (t) => {
  await t.test('launch failure', async () => {
    const child = fakeProcess();
    const killer = new EventEmitter();
    const stop = terminateChild(child, 1000, { platform: 'win32', spawnImpl: () => killer });
    killer.emit('error', new Error('spawn ENOENT'));
    await assert.rejects(stop, /spawn ENOENT/);
    assert.equal(child.listenerCount('exit'), 0);
  });
  await t.test('access denied', async () => {
    const child = fakeProcess();
    const killer = new EventEmitter();
    const stop = terminateChild(child, 1000, { platform: 'win32', spawnImpl: () => killer });
    killer.emit('close', 5);
    await assert.rejects(stop, /进程树失败（5）/);
  });
});

test('Windows shutdown has a deadline when the tree command or parent never exits', async (t) => {
  for (const commandExits of [false, true]) {
    await t.test(`tree command exits=${commandExits}`, async () => {
      const child = fakeProcess();
      const killer = Object.assign(new EventEmitter(), { kill() {} });
      const stop = terminateChild(child, 30, { platform: 'win32', spawnImpl: () => killer });
      if (commandExits) killer.emit('close', 0);
      await assert.rejects(stop, /超时|及时停止/);
      assert.equal(child.listenerCount('exit'), 0);
    });
  }
});

test('already exited processes do not run a shutdown command', async () => {
  const child = fakeProcess();
  child.exitCode = 0;
  await terminateChild(child, 100, { spawnImpl: () => assert.fail('exited process') });
});

test('Windows shutdown removes a real detached descendant before resolving', { skip: process.platform !== 'win32', timeout: 20000 }, async () => {
  const parent = spawn(process.execPath, ['-e', `
    const child = require('node:child_process').spawn(process.execPath,
      ['-e', 'console.log("ready");setInterval(()=>{},1000)'],
      { detached: true, stdio: ['ignore', 'pipe', 'ignore'], windowsHide: true });
    child.stdout.once('data', () => console.log(child.pid));
    setInterval(()=>{},1000);
  `], { stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true });
  let descendant;
  try {
    const [chunk] = await once(parent.stdout, 'data');
    descendant = Number(String(chunk).trim());
    assert.ok(Number.isInteger(descendant) && descendant > 0);
    await terminateChild(parent);
    assert.throws(() => process.kill(descendant, 0), /ESRCH/);
  } finally {
    if (descendant) { try { process.kill(descendant); } catch {} }
    try { parent.kill(); } catch {}
  }
});
