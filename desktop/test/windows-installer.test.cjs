const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn, execFile } = require('node:child_process');
const { once } = require('node:events');
const { promisify } = require('node:util');
const test = require('node:test');

const execute = promisify(execFile);
const helper = path.resolve(__dirname, '../scripts/stop-installed-app.ps1');
const windows = { skip: process.platform !== 'win32', timeout: 30000 };

async function prepare(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "easy-stock install's-"));
  const app = path.join(root, 'app');
  fs.mkdirSync(app);
  fs.copyFileSync(process.execPath, path.join(app, 'easy-stock.exe'));
  const owned = [];
  t.after(async () => {
    for (const child of owned) {
      try { process.kill(typeof child === 'number' ? child : child.pid); } catch {}
    }
    for (let attempt = 0; attempt < 50; attempt += 1) {
      try { fs.rmSync(root, { recursive: true, force: true }); return; }
      catch (error) { if (attempt === 49) throw error; }
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
  });
  return { root, app, owned };
}

async function startFixture(executable, owned, childExecutable) {
  const program = childExecutable
    ? `const c=require('node:child_process').spawn(${JSON.stringify(childExecutable)},['-e','console.log("ready");setInterval(()=>{},1000)'],{detached:true,stdio:['ignore','pipe','ignore'],windowsHide:true});c.stdout.once('data',()=>console.log(c.pid));setInterval(()=>{},1000)`
    : 'console.log("ready");setInterval(()=>{},1000)';
  const child = spawn(executable, ['-e', program], { stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true });
  owned.push(child);
  const [chunk] = await once(child.stdout, 'data');
  if (childExecutable) {
    const descendant = Number(String(chunk).trim());
    assert.ok(Number.isInteger(descendant) && descendant > 0);
    owned.push(descendant);
    return { child, descendant };
  }
  return { child };
}

function runHelper(directory, extra = []) {
  return execute('powershell.exe', ['-NoLogo', '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass',
    '-File', helper, '-InstallDirectory', directory, '-GracePeriodSeconds', '0', '-StopTimeoutSeconds', '2', ...extra],
  { windowsHide: true, timeout: 20000 });
}

test('installer embeds the path-scoped helper in both install and uninstall checks', () => {
  const builder = fs.readFileSync(path.resolve(__dirname, '../scripts/electron-builder.mjs'), 'utf8');
  const hook = fs.readFileSync(path.resolve(__dirname, '../assets/installer.nsh'), 'utf8');
  assert.match(builder, /include: path\.join\(desktopRoot, 'assets', 'installer\.nsh'\)/);
  assert.match(hook, /!macro customCheckAppRunning/);
  assert.match(hook, /File .*stop-installed-app\.ps1/);
  assert.match(hook, /-NoProfile -NonInteractive -ExecutionPolicy Bypass -File/);
  assert.match(hook, /-InstallDirectory "\$INSTDIR"/);
  assert.match(hook, /SetErrorLevel 2[\s\S]*Quit/);
  assert.match(hook, /!ifndef BUILD_UNINSTALLER[\s\S]*\$\{isUpdated\}[\s\S]*repair-legacy-windows-paths\.ps1/);
});

test('upgrade removes long legacy Hermes document paths before the old uninstaller runs', windows, async (t) => {
  const { root } = await prepare(t);
  const app = path.join(root, 'installed-easy-stock-with-a-long-enough-path-for-windows-nsis-upgrade-test');
  fs.mkdirSync(app);
  fs.writeFileSync(path.join(app, 'Uninstall easy-stock.exe'), 'fixture');
  const website = path.join(app, 'resources', 'resources', 'hermes-runtime', 'source', 'website');
  const longFile = path.join(website, 'i18n', 'zh-Hans', 'docusaurus-plugin-content-docs', 'current', 'user-guide', 'skills', 'bundled', 'software-development', 'software-development-hermes-agent-skill-authoring.md');
  assert.ok(longFile.length > 260, 'fixture must exceed MAX_PATH');
  const extended = (value) => `\\\\?\\${value}`;
  fs.mkdirSync(extended(path.dirname(longFile)), { recursive: true });
  fs.writeFileSync(extended(longFile), 'old documentation');
  fs.mkdirSync(path.join(website, 'docs'), { recursive: true });
  fs.writeFileSync(path.join(website, 'docs', 'guide.md'), 'old documentation');
  fs.mkdirSync(path.join(website, 'static'), { recursive: true });
  fs.writeFileSync(path.join(website, 'static', 'model-catalog.json'), 'keep');
  const sibling = `${app}-backup`;
  fs.mkdirSync(sibling);
  fs.writeFileSync(path.join(sibling, 'keep'), 'keep');

  const script = path.resolve(__dirname, '../scripts/repair-legacy-windows-paths.ps1');
  const { stdout } = await execute('powershell.exe', ['-NoLogo', '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', script, '-InstallDirectory', app], { windowsHide: true, timeout: 30000 });
  assert.match(stdout, /Legacy Hermes documentation paths removed/);
  assert.equal(fs.existsSync(extended(longFile)), false);
  assert.equal(fs.existsSync(path.join(website, 'docs')), false);
  assert.equal(fs.readFileSync(path.join(website, 'static', 'model-catalog.json'), 'utf8'), 'keep');
  assert.equal(fs.readFileSync(path.join(sibling, 'keep'), 'utf8'), 'keep');
  assert.equal(fs.existsSync(path.join(app, 'Uninstall easy-stock.exe')), true);
});

test('installer closes its directory and detached descendants without touching a prefix sibling', windows, async (t) => {
  const { app, owned } = await prepare(t);
  const sibling = `${app}-backup`;
  fs.mkdirSync(sibling);
  fs.copyFileSync(process.execPath, path.join(sibling, 'easy-stock.exe'));
  const target = await startFixture(path.join(app, 'easy-stock.exe'), owned, process.execPath);
  const outside = await startFixture(path.join(sibling, 'easy-stock.exe'), owned);
  const sentinel = path.join(app, 'test-user-data.txt');
  fs.writeFileSync(sentinel, 'isolated fixture: preserve on upgrade');
  const result = await runHelper(app);
  assert.match(result.stdout, /Installation processes stopped; verified [1-9]\d* process identities\./);
  assert.throws(() => process.kill(target.child.pid, 0), /ESRCH/, result.stdout);
  assert.throws(() => process.kill(target.descendant, 0), /ESRCH/, result.stdout);
  assert.doesNotThrow(() => process.kill(outside.child.pid, 0));
  assert.equal(fs.readFileSync(sentinel, 'utf8'), 'isolated fixture: preserve on upgrade');
});

test('installer matches long and DOS short installation paths in both directions', windows, async (t) => {
  const { app, owned } = await prepare(t);
  const script = `
Add-Type -TypeDefinition @'
using System.Runtime.InteropServices;
using System.Text;
public static class FixturePaths {
  [DllImport("kernel32.dll", CharSet = CharSet.Unicode)]
  public static extern uint GetLongPathName(string path, StringBuilder result, uint capacity);
  [DllImport("kernel32.dll", CharSet = CharSet.Unicode)]
  public static extern uint GetShortPathName(string path, StringBuilder result, uint capacity);
}
'@
$long = [Text.StringBuilder]::new(32768)
$short = [Text.StringBuilder]::new(32768)
if (-not [FixturePaths]::GetLongPathName('${app.replaceAll("'", "''")}', $long, $long.Capacity)) { throw 'Cannot expand fixture path' }
if (-not [FixturePaths]::GetShortPathName($long.ToString(), $short, $short.Capacity)) { throw 'Cannot shorten fixture path' }
@{ long = $long.ToString(); short = $short.ToString() } | ConvertTo-Json -Compress
`;
  const { stdout } = await execute('powershell.exe', ['-NoLogo', '-NoProfile', '-NonInteractive', '-EncodedCommand', Buffer.from(script, 'utf16le').toString('base64')], { windowsHide: true, timeout: 20000 });
  const paths = JSON.parse(stdout.trim());
  if (paths.long.toLowerCase() === paths.short.toLowerCase()) {
    t.skip('DOS short names are unavailable on this volume');
    return;
  }
  for (const [install, launch] of [[paths.long, paths.short], [paths.short, paths.long]]) {
    const fixture = await startFixture(path.join(launch, 'easy-stock.exe'), owned);
    const result = await runHelper(install);
    assert.match(result.stdout, /verified [1-9]\d* process identities\./);
    assert.throws(() => process.kill(fixture.child.pid, 0), /ESRCH/, result.stdout);
  }
});

test('installer excludes its own process identity from directory cleanup', windows, async (t) => {
  const { app, owned } = await prepare(t);
  const protectedProcess = await startFixture(path.join(app, 'easy-stock.exe'), owned);
  await runHelper(app, ['-InstallerProcessId', String(protectedProcess.child.pid)]);
  assert.doesNotThrow(() => process.kill(protectedProcess.child.pid, 0));
});

test('installer cleans a remaining backend even when an interrupted install removed the main executable', windows, async (t) => {
  const { app, owned } = await prepare(t);
  const backend = path.join(app, 'resources', 'resources', 'backend', 'easy-stock-backend.exe');
  fs.mkdirSync(path.dirname(backend), { recursive: true });
  fs.renameSync(path.join(app, 'easy-stock.exe'), backend);
  const fixture = await startFixture(backend, owned);
  await runHelper(app);
  assert.throws(() => process.kill(fixture.child.pid, 0), /ESRCH/);
});

test('installer refuses a volume root and does not require an existing install', windows, async (t) => {
  const { root } = await prepare(t);
  await assert.rejects(runHelper(path.parse(root).root), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout, /volume root/);
    return true;
  });
  await assert.rejects(runHelper('C:relative-app'), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout, /not absolute/);
    return true;
  });
  const result = await runHelper(path.join(root, 'not-installed'));
  assert.match(result.stdout, /No previous installation/);
});

test('installer refuses a junction instead of acting on an unexpected target', windows, async (t) => {
  const { root, app, owned } = await prepare(t);
  const fixture = await startFixture(path.join(app, 'easy-stock.exe'), owned);
  const link = path.join(root, 'linked-app');
  fs.symlinkSync(app, link, 'junction');
  await assert.rejects(runHelper(link), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout, /symbolic link or junction/);
    return true;
  });
  assert.doesNotThrow(() => process.kill(fixture.child.pid, 0));
});
