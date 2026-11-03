const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const test = require('node:test');
const vm = require('node:vm');

let fixtureRoot;
let fakeBrowser;
let python;
test.before(() => {
  fixtureRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'a-stock browser 中文 '));
  const runtimeRoot = path.resolve(__dirname, '..', 'resources', 'hermes-runtime');
  const candidates = [process.env.EASY_STOCK_HERMES_TEST_PYTHON, process.env.A_STOCK_HERMES_PYTHON,
    path.join(runtimeRoot, process.platform === 'win32' ? 'python/python.exe' : 'venv/bin/python'), 'python3', 'python'].filter(Boolean);
  python = candidates.find((candidate) => spawnSync(candidate, ['-c', 'pass'], { windowsHide: true }).status === 0);
  assert.ok(python, 'a working Python interpreter is required to test the browser wrapper');
  fakeBrowser = path.join(fixtureRoot, process.platform === 'win32' ? 'browser.exe' : 'browser');
  const source = path.join(fixtureRoot, 'main.go');
  fs.writeFileSync(source, `package main
import ("encoding/json"; "fmt"; "os")
func main() {
  log, err := os.OpenFile(os.Getenv("A_STOCK_TEST_CALL_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
  if err != nil { panic(err) }
  json.NewEncoder(log).Encode(os.Args[1:]); log.Close()
  for _, arg := range os.Args[1:] {
    if arg == "load" && os.Getenv("A_STOCK_TEST_STATE_FAIL") == "1" { os.Exit(7) }
  }
  fmt.Print(\`{"success":true}\`)
}
`);
  const built = spawnSync('go', ['build', '-o', fakeBrowser, source], {
    encoding: 'utf8', windowsHide: true, timeout: 60000, env: { ...process.env, GOMAXPROCS: '2' },
  });
  assert.equal(built.status, 0, built.error?.message || built.stderr);
});
test.after(() => { if (fixtureRoot) fs.rmSync(fixtureRoot, { recursive: true, force: true }); });

function browserFixture(t) {
  const root = fs.mkdtempSync(path.join(fixtureRoot, 'case '));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const logPath = path.join(root, 'calls.log');
  const statePath = path.join(root, 'state 中文.json');
  const socketDir = path.join(root, 'socket');
  fs.writeFileSync(statePath, '{"cookies":[],"origins":[]}');
  const wrapper = path.resolve(__dirname, '..', 'scripts', 'browser-bin', 'agent-browser');
  const env = { ...process.env, A_STOCK_AGENT_BROWSER_REAL: fakeBrowser, A_STOCK_TEST_CALL_LOG: logPath,
    AGENT_BROWSER_STATE: statePath, AGENT_BROWSER_SOCKET_DIR: socketDir };
  const run = (args, overrides = {}) => spawnSync(python, [wrapper, '--session', 'test-session', '--json', ...args], {
    encoding: 'utf8', windowsHide: true, timeout: 15000, env: { ...env, ...overrides },
  });
  const calls = () => fs.readFileSync(logPath, 'utf8').trim().split(/\r?\n/).map((line) => JSON.parse(line));
  return { root, statePath, run, calls };
}

test('agent-browser wrapper loads storage state before the first navigation', (t) => {
  const fixture = browserFixture(t);
  const url = 'https://example.invalid/path with spaces?value=100%25&name=中文';
  const result = fixture.run(['open', url]);
  assert.equal(result.status, 0, result.error?.message || result.stderr);
  assert.deepEqual(JSON.parse(result.stdout), { success: true });
  const prefix = ['--session', 'test-session', '--json'];
  assert.deepEqual(fixture.calls(), [
    [...prefix, 'open', 'about:blank'],
    [...prefix, 'state', 'load', fs.realpathSync(fixture.statePath)],
    [...prefix, 'open', url],
  ]);
  assert.equal(fixture.run(['open', url]).status, 0);
  const script = 'const x = "100% 中文";\nconsole.log(x);';
  assert.equal(fixture.run(['eval', script]).status, 0);
  assert.deepEqual(fixture.calls().slice(3), [[...prefix, 'open', url], [...prefix, 'eval', script]]);
});

test('agent-browser wrapper stops navigation when loading storage state fails', (t) => {
  const fixture = browserFixture(t);
  const result = fixture.run(['open', 'https://example.invalid/'], { A_STOCK_TEST_STATE_FAIL: '1' });
  assert.equal(result.status, 7, result.stderr);
  assert.equal(fixture.calls().length, 2);
  assert.equal(fixture.calls()[1].at(-3), 'state');
});

test('desktop resolves packaged Python wrappers without falling back to absent development scripts', (t) => {
  const fixture = browserFixture(t);
  const main = fs.readFileSync(path.resolve(__dirname, '..', 'main.cjs'), 'utf8');
  const start = main.indexOf('function buildRuntimeEnv(');
  const end = main.indexOf('\nasync function bootXueqiuBrowserBridge', start);
  assert.ok(start >= 0 && end > start);
  const resourcesRoot = path.join(fixture.root, 'resources');
  const wrapperDir = path.join(resourcesRoot, 'agent-browser');
  fs.mkdirSync(wrapperDir, { recursive: true });
  fs.writeFileSync(path.join(wrapperDir, 'agent-browser.py'), '# packaged fixture');
  fs.copyFileSync(fakeBrowser, path.join(wrapperDir, process.platform === 'win32' ? 'agent-browser-real.exe' : 'agent-browser-real'));
  const appDirectory = path.join(fixture.root, 'app.asar');
  const buildRuntimeEnv = vm.runInNewContext('(' + main.slice(start, end) + ')', {
    fs, path, process: { env: {}, platform: process.platform }, __dirname: appDirectory,
    app: { getPath: () => path.join(fixture.root, 'user'), getVersion: () => 'test', isPackaged: true },
    runtimeLogDirectory: path.join(fixture.root, 'logs'), resolveHermesRuntimeRoot: () => path.join(resourcesRoot, 'hermes-runtime'),
    xueqiuBrowserBridgeConfig: null, taogubaBrowserBridgeConfig: null,
    eastmoneySessionBridgeConfig: null, eastmoneyPush2BridgeConfig: null, wechatServiceConfig: null,
    agentBrowserBinaryName: () => 'unused',
  });
  const env = buildRuntimeEnv(resourcesRoot);
  assert.equal(env.A_STOCK_AGENT_BROWSER_WRAPPER_DIR, wrapperDir);
  assert.equal(fs.existsSync(path.join(appDirectory, 'scripts')), false);
});
