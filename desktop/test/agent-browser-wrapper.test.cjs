const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const test = require('node:test');

test('agent-browser wrapper loads storage state before the first navigation', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'a-stock-browser-wrapper-'));
  const logPath = path.join(root, 'calls.log');
  const statePath = path.join(root, 'state.json');
  const socketDir = path.join(root, 'socket');
  const fakeBrowser = path.join(root, 'agent-browser-real');
  fs.writeFileSync(statePath, '{"cookies":[],"origins":[]}');
  fs.writeFileSync(fakeBrowser, `#!/bin/sh\nprintf '%s\\n' "$*" >> "$A_STOCK_TEST_CALL_LOG"\nexit 0\n`, { mode: 0o755 });

  const wrapper = path.resolve(__dirname, '..', 'scripts', 'browser-bin', 'agent-browser');
  const result = spawnSync(wrapper, ['--session', 'test-session', '--json', 'open', 'https://xueqiu.com/'], {
    encoding: 'utf8',
    env: {
      ...process.env,
      A_STOCK_AGENT_BROWSER_REAL: fakeBrowser,
      A_STOCK_TEST_CALL_LOG: logPath,
      AGENT_BROWSER_STATE: statePath,
      AGENT_BROWSER_SOCKET_DIR: socketDir,
    },
  });

  assert.equal(result.status, 0, result.stderr);
  const calls = fs.readFileSync(logPath, 'utf8').trim().split('\n');
  const resolvedStatePath = fs.realpathSync(statePath);
  assert.deepEqual(calls, [
    '--session test-session --json open about:blank',
    `--session test-session --json state load ${resolvedStatePath}`,
    '--session test-session --json open https://xueqiu.com/',
  ]);
});
