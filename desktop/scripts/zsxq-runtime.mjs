import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import runtime from '../zsxq-runtime.cjs';

export const { zsxqExecutable } = runtime;
const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
export const zsxqLock = JSON.parse(fs.readFileSync(path.join(desktopRoot, 'zsxq-runtime-lock.json'), 'utf8'));

export function verifyZsxqArchive(bytes, integrity) {
  if (`sha512-${createHash('sha512').update(bytes).digest('base64')}` !== integrity) throw new Error('Knowledge planet CLI archive integrity mismatch');
}

export async function prepareZsxqRuntime({ runtimeRoot, platform = process.platform, arch = process.arch, archive = process.env.ZSXQ_RUNTIME_ARCHIVE } = {}) {
  if (!runtimeRoot) throw new Error('runtimeRoot is required');
  if (platform !== process.platform || arch !== process.arch) throw new Error('Knowledge planet CLI must be prepared on its target platform and architecture');
  const asset = zsxqLock.platforms[`${platform}-${arch}`];
  if (!asset) throw new Error(`Unsupported knowledge planet CLI platform: ${platform}-${arch}`);
  runtimeRoot = path.resolve(runtimeRoot);
  fs.mkdirSync(path.dirname(runtimeRoot), { recursive: true });
  const staging = fs.mkdtempSync(path.join(path.dirname(runtimeRoot), '.zsxq-build-'));
  try {
    let bytes;
    if (archive) bytes = fs.readFileSync(archive);
    else {
      const response = await fetch(asset.url, { signal: AbortSignal.timeout(120000) });
      if (!response.ok) throw new Error(`Knowledge planet CLI download failed: HTTP ${response.status}`);
      bytes = Buffer.from(await response.arrayBuffer());
    }
    verifyZsxqArchive(bytes, asset.integrity);
    fs.writeFileSync(path.join(staging, 'runtime.tgz'), bytes);
    const binaryName = platform === 'win32' ? 'zsxq-cli.exe' : 'zsxq-cli';
    const allowed = new Set(['package/package.json', `package/bin/${binaryName}`]);
    const listing = command('tar', ['-tzf', 'runtime.tgz'], { cwd: staging }).stdout.trim().split(/\r?\n/);
    if (listing.length !== allowed.size || listing.some(entry => !allowed.has(entry))) throw new Error('Unexpected knowledge planet CLI archive layout');
    command('tar', ['-xzf', 'runtime.tgz'], { cwd: staging });
    const sourceRoot = path.join(staging, 'package');
    for (const entry of ['package.json', 'bin', `bin/${binaryName}`]) {
      if (fs.lstatSync(path.join(sourceRoot, entry)).isSymbolicLink()) throw new Error('Knowledge planet CLI archive must contain regular files');
    }
    const pkg = JSON.parse(fs.readFileSync(path.join(sourceRoot, 'package.json'), 'utf8'));
    if (pkg.name !== asset.package || pkg.version !== zsxqLock.version || pkg.license !== 'Apache-2.0') throw new Error('Knowledge planet CLI package does not match the locked release');
    const builtRoot = path.join(staging, 'runtime');
    const executable = zsxqExecutable(builtRoot, platform);
    fs.mkdirSync(path.dirname(executable), { recursive: true });
    fs.copyFileSync(path.join(sourceRoot, 'bin', binaryName), executable);
    if (platform !== 'win32') fs.chmodSync(executable, 0o755);
    for (const name of ['LICENSE', 'NOTICE']) fs.copyFileSync(path.join(desktopRoot, 'licenses', `zsxq-${name}`), path.join(builtRoot, name));
    const manifest = { version: zsxqLock.version, package: asset.package, platform, arch, source: asset.url, integrity: asset.integrity };
    fs.writeFileSync(path.join(builtRoot, 'runtime-manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
    verifyZsxqRuntime(builtRoot);
    const previous = path.join(staging, 'previous');
    const hadPrevious = fs.existsSync(runtimeRoot);
    if (hadPrevious) fs.renameSync(runtimeRoot, previous);
    try { fs.renameSync(builtRoot, runtimeRoot); }
    catch (error) { if (hadPrevious) fs.renameSync(previous, runtimeRoot); throw error; }
    return manifest;
  } finally { fs.rmSync(staging, { recursive: true, force: true }); }
}

export function verifyZsxqRuntime(root) {
  const manifest = JSON.parse(fs.readFileSync(path.join(root, 'runtime-manifest.json'), 'utf8'));
  const asset = zsxqLock.platforms[`${process.platform}-${process.arch}`];
  if (!asset || manifest.version !== zsxqLock.version || manifest.package !== asset.package || manifest.platform !== process.platform || manifest.arch !== process.arch || manifest.source !== asset.url || manifest.integrity !== asset.integrity) throw new Error('Knowledge planet CLI runtime does not match the locked target');
  for (const file of ['LICENSE', 'NOTICE']) {
    if (!fs.statSync(path.join(root, file)).isFile()) throw new Error(`Knowledge planet CLI ${file} is missing`);
  }
  const executable = zsxqExecutable(root);
  if (!fs.lstatSync(executable).isFile()) throw new Error('Knowledge planet CLI native executable is missing');
  // Match Finder/installed Windows apps: no npm, shell profile, or external Node.
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => key.toUpperCase() !== 'PATH'));
  env.PATH = process.platform === 'win32' ? path.join(process.env.SystemRoot || 'C:\\Windows', 'System32') : '/usr/bin:/bin';
  const version = command(executable, ['--version'], { env }).stdout.trim();
  if (version !== `zsxq-cli version ${zsxqLock.version}`) throw new Error(`Unexpected knowledge planet CLI version: ${version}`);
  const help = command(executable, ['auth', 'login', '--help'], { env }).stdout;
  if (!['--device-code', '--no-browser', '--no-wait'].every(flag => help.includes(flag))) throw new Error('Knowledge planet CLI is missing device authorization support');
  return manifest;
}

function command(executable, args, options = {}) {
  const result = spawnSync(executable, args, { encoding: 'utf8', timeout: 15000, windowsHide: true, ...options });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${path.basename(executable)} failed (${result.status}): ${result.stderr || result.stdout}`);
  return result;
}
