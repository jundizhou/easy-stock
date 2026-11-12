const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn, execFile } = require('node:child_process');
const { once } = require('node:events');
const { promisify } = require('node:util');
const { pathToFileURL } = require('node:url');
const { gunzipSync } = require('node:zlib');
const { createHash } = require('node:crypto');
const test = require('node:test');

const execute = promisify(execFile);
const helper = path.resolve(__dirname, '../scripts/stop-installed-app.ps1');
const windows = { skip: process.platform !== 'win32', timeout: 30000 };

async function builderFixture(t, platform, mode = 'release') {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock $builder-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const scripts = path.join(root, 'scripts');
  fs.mkdirSync(scripts);
  fs.copyFileSync(path.resolve(__dirname, '../scripts/electron-builder.mjs'), path.join(scripts, 'electron-builder.mjs'));
  fs.writeFileSync(path.join(scripts, 'builder-filesystem.mjs'), '');
  fs.copyFileSync(path.resolve(__dirname, '../package.json'), path.join(root, 'package.json'));
  fs.mkdirSync(path.join(root, 'assets'));
  fs.copyFileSync(path.resolve(__dirname, '../assets/installer.nsh'), path.join(root, 'assets/installer.nsh'));
  fs.writeFileSync(path.join(root, 'update-feed.cjs'), 'exports.resolveUpdateFeed = () => ({ provider: "generic", url: "https://example.invalid" });');
  const mock = path.join(root, 'mock-builder.mjs');
  const extractorRoot = path.join(root, '7zip toolset');
  fs.mkdirSync(path.join(extractorRoot, 'bin'), { recursive: true });
  fs.writeFileSync(path.join(extractorRoot, 'bin', '7za.exe'), 'fixture');
  fs.writeFileSync(path.join(extractorRoot, 'LICENSE.txt'), 'fixture license');
  fs.writeFileSync(path.join(extractorRoot, 'COPYING'), 'fixture license');
  fs.writeFileSync(mock, `
    import fs from 'node:fs'; import path from 'node:path';
    export const Arch = { x64: 'x64', arm64: 'arm64' };
    export const Platform = Object.fromEntries(['MAC', 'WINDOWS'].map(name => [name, {
      createTarget: (targets, arch) => ({ platform: name, targets, arch })
    }]));
    export async function build(options) {
      fs.writeFileSync('captured.json', JSON.stringify(options));
      const artifact = path.join(options.config.directories.output, options.config.nsis.artifactName);
      fs.writeFileSync(artifact, Buffer.alloc(100003, 37));
      return [artifact];
    }
  `);
  const toolsetMock = path.join(root, 'mock-toolset.mjs');
  fs.writeFileSync(toolsetMock, `
    import assert from 'node:assert/strict';
    export async function downloadBuilderToolset(options) {
      assert.equal(options.releaseName, '7zip@1.0.0');
      assert.equal(options.filenameWithExt, '7zip-win-x64.tar.gz');
      assert.equal(options.checksums[options.filenameWithExt], 'be071f15bd6da2f78fe81c6ddef2009b0c4d8a51f36b780cb806c7e6df95e1b3');
      assert.equal(options.githubOrgRepo, 'electron-userland/electron-builder-binaries');
      return ${JSON.stringify(extractorRoot)};
    }
  `);
  const loader = path.join(root, 'loader.mjs');
  const blockmapModule = pathToFileURL(require.resolve('app-builder-lib/out/targets/blockmap/blockmap.js')).href;
  fs.writeFileSync(loader, `
    import { registerHooks } from 'node:module';
    const modules = ${JSON.stringify({ 'electron-builder': pathToFileURL(mock).href, 'app-builder-lib/out/util/electronGet.js': pathToFileURL(toolsetMock).href, 'app-builder-lib/out/targets/blockmap/blockmap.js': blockmapModule })};
    registerHooks({ resolve(specifier, context, next) {
      return modules[specifier] ? { url: modules[specifier], shortCircuit: true } : next(specifier, context);
    }});
  `);
  await execute(process.execPath, ['--import', pathToFileURL(loader).href, path.join(scripts, 'electron-builder.mjs'), platform, mode], {
    cwd: root, windowsHide: true, timeout: 30000,
    env: { ...process.env, A_STOCK_DESKTOP_ARCH: 'x64', CSC_LINK: '' },
  });
  const options = JSON.parse(fs.readFileSync(path.join(root, 'captured.json'), 'utf8'));
  return { root, options, artifact: path.join(options.config.directories.output, options.config.nsis.artifactName) };
}

test('Windows release uses standalone direct extraction and keeps a valid installer blockmap', async (t) => {
  const { options, artifact } = await builderFixture(t, 'windows');
  assert.equal(options.config.nsis.useZip, true);
  assert.equal(options.config.nsis.differentialPackage, false);
  assert.equal(options.config.nsis.deleteAppDataOnUninstall, false);
  assert.equal(options.publish, 'never');
  assert.deepEqual(options.targets, { platform: 'WINDOWS', targets: ['nsis'], arch: 'x64' });
  const hook = fs.readFileSync(options.config.nsis.include, 'utf8');
  const extraction = fs.readFileSync(path.join(options.config.directories.output, 'easy-stock-direct-extract.nsh'), 'utf8');
  assert.match(hook, /-InstallDirectory "\$INSTDIR"/);
  assert.match(hook, /!ifndef BUILD_UNINSTALLER\s+!include/);
  assert.match(extraction, /File \/oname=\$PLUGINSDIR\\easy-stock-7za\.exe "[^"\r\n]+7za\.exe"/);
  assert.ok(extraction.includes(String.raw`nsExec::ExecToLog '"$PLUGINSDIR\easy-stock-7za.exe" x "$PLUGINSDIR\app-$packageArch.zip" "-o$INSTDIR" -y -bso0 -bsp0'`));
  assert.match(extraction, /Pop \$R0\s+StrCmp \$R0 "0" easyStockExtractionDone/);
  assert.doesNotMatch(extraction, /nsisunz|Nsis7z|CopyFiles|7z-out|LoopExtract/);
  assert.match(extraction, /File \/oname=7zip-LICENSE\.txt/);
  assert.match(extraction, /File \/oname=7zip-COPYING\.txt/);
  assert.ok(extraction.includes('$$builder-'), 'compiler paths must escape literal dollar signs');
  assert.ok(hook.includes('$$builder-'), 'include paths must escape literal dollar signs');
  assert.match(extraction, /\/SD IDOK/);
  assert.match(extraction, /SetErrorLevel 2\s+Quit/);
  assert.match(hook, /!insertmacro easyStockPrepareUpgradeTemp/);
  assert.match(extraction, /GetTempFileName \$R9 "\$R8"/);
  assert.match(extraction, /ReadEnvStr \$easyStockPreviousTemp "TEMP"/);
  assert.match(extraction, /SetEnvironmentVariableW\(w "TEMP", w "\$easyStockPreviousTemp"\)/);
  assert.match(extraction, /!macro customInstall\s+!insertmacro easyStockRestoreUpgradeTemp\s+!insertmacro easyStockFinishInstallProgress/);
  assert.match(extraction, /!insertmacro easyStockStartInstallProgress\s+File \/oname=\$PLUGINSDIR\\easy-stock-7za\.exe/);
  assert.equal((extraction.match(/!insertmacro easyStockStartInstallProgress/g) || []).length, 1);
  assert.doesNotMatch(hook, /!insertmacro easyStockStartInstallProgress/);
  assert.match(extraction, /IfSilent easyStockProgressStarted/);
  assert.match(extraction, /SendMessage \$mui\.InstFilesPage\.ProgressBar 0x40A 1 30/);
  assert.match(extraction, /SendMessage \$mui\.InstFilesPage\.ProgressBar 0x40A 0 0/);
  const map = JSON.parse(gunzipSync(fs.readFileSync(`${artifact}.blockmap`)));
  assert.equal(map.version, '2');
  assert.equal(map.files[0].sizes.reduce((sum, size) => sum + size, 0), fs.statSync(artifact).size);
  assert.equal(map.files[0].checksums.length, map.files[0].sizes.length);
  assert.equal(createHash('sha256').update(fs.readFileSync(artifact)).digest('hex'),
    createHash('sha256').update(Buffer.alloc(100003, 37)).digest('hex'));
});

test('directory and macOS builds do not generate Windows installer blockmaps', async (t) => {
  for (const [platform, mode] of [['windows', 'dir'], ['mac', 'release']]) {
    const { artifact } = await builderFixture(t, platform, mode);
    assert.equal(fs.existsSync(`${artifact}.blockmap`), false);
  }
});

test('desktop package version and workspace lock remain synchronized', () => {
  const manifest = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../package.json'), 'utf8'));
  const lock = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../package-lock.json'), 'utf8'));
  assert.equal(lock.packages.desktop.version, manifest.version);
});

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
