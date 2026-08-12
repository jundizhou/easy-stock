import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const packageManifest = JSON.parse(fs.readFileSync(path.join(desktopRoot, 'package.json'), 'utf8'));
const platform = process.argv[2];
const mode = process.argv[3] || 'release';
const arch = process.env.A_STOCK_DESKTOP_ARCH || process.arch;
if (!['mac', 'windows'].includes(platform) || !['release', 'dir'].includes(mode)) throw new Error('Usage: node electron-builder.mjs <mac|windows> <release|dir>');
if (!['arm64', 'x64'].includes(arch)) throw new Error(`Unsupported desktop architecture: ${arch}`);

const outputDirectory = path.join(desktopRoot, 'dist', mode === 'dir' ? 'builder-dir' : 'builder-release');
const hasMacNotarizationCredentials = Boolean(
  process.env.CSC_LINK && (
    (process.env.APPLE_ID && process.env.APPLE_APP_SPECIFIC_PASSWORD && process.env.APPLE_TEAM_ID)
    || process.env.APPLE_API_KEY
    || process.env.APPLE_KEYCHAIN
  ),
);
fs.mkdirSync(outputDirectory, { recursive: true });
if (mode === 'release') {
  for (const entry of fs.readdirSync(outputDirectory)) {
    if (entry === 'builder-debug.yml') continue;
    fs.rmSync(path.join(outputDirectory, entry), { recursive: true, force: true });
  }
}
const config = {
  appId: 'com.jundizhou.easystock',
  productName: 'easy-stock',
  electronVersion: process.env.A_STOCK_ELECTRON_VERSION || packageManifest.devDependencies.electron,
  ...(process.env.A_STOCK_ELECTRON_DIST ? { electronDist: process.env.A_STOCK_ELECTRON_DIST } : {}),
  copyright: 'Copyright © easy-stock contributors',
  directories: { output: outputDirectory, buildResources: path.join(desktopRoot, 'assets') },
  files: [
    'main.cjs',
    'preload.cjs',
    'review-login-preload.cjs',
    'xueqiu-login-preload.cjs',
    'backend-process.cjs',
    'browser-auth.cjs',
    'data-protection.cjs',
    'hermes-runtime-root.cjs',
    'taoguba-browser-bridge.cjs',
    'update-manager.cjs',
    'user-data.cjs',
    'wechat-service.cjs',
    'xueqiu-browser-bridge.cjs',
    'package.json',
    '!dist{,/**/*}',
    '!resources{,/**/*}',
    '!scripts{,/**/*}',
    '!test{,/**/*}',
  ],
  extraResources: [{ from: path.join(desktopRoot, 'resources'), to: 'resources' }],
  asar: true,
  publish: [{ provider: 'github', owner: 'jundizhou', repo: 'easy-stock', releaseType: 'release' }],
  mac: {
    category: 'public.app-category.finance',
    icon: path.join(desktopRoot, 'assets', 'easy-stock.icns'),
    target: mode === 'dir' ? [{ target: 'dir', arch: [arch] }] : [{ target: 'zip', arch: [arch] }],
    artifactName: `easy-stock-v${packageManifest.version}-macos-${arch}.\${ext}`,
    hardenedRuntime: true,
    gatekeeperAssess: false,
    identity: process.env.CSC_LINK ? undefined : null,
    notarize: hasMacNotarizationCredentials,
  },
  dmg: { artifactName: `easy-stock-v${packageManifest.version}-macos-${arch}.dmg` },
  win: {
    icon: path.join(desktopRoot, 'assets', 'easy-stock.ico'),
    target: mode === 'dir' ? [{ target: 'dir', arch: [arch] }] : [{ target: 'nsis', arch: [arch] }],
    artifactName: `easy-stock-v${packageManifest.version}-windows-${arch}.\${ext}`,
    ...(process.env.CSC_LINK ? {} : { signAndEditExecutable: false }),
  },
  nsis: {
    oneClick: true,
    perMachine: false,
    allowElevation: true,
    deleteAppDataOnUninstall: false,
    artifactName: `easy-stock-v${packageManifest.version}-windows-${arch}-setup.exe`,
  },
};

const { Arch, build, Platform } = await import('electron-builder');
const targetPlatform = platform === 'mac' ? Platform.MAC : Platform.WINDOWS;
const targetArch = Arch[arch];
await build({
  targets: mode === 'dir'
    ? targetPlatform.createTarget(['dir'], targetArch)
    : targetPlatform.createTarget(platform === 'mac' ? ['zip'] : ['nsis'], targetArch),
  config,
  projectDir: desktopRoot,
  publish: 'never',
});
