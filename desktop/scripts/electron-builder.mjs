import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const packageManifest = JSON.parse(fs.readFileSync(path.join(desktopRoot, 'package.json'), 'utf8'));
const updateFeedURL = process.env.A_STOCK_UPDATE_FEED_URL || 'https://easy-stock-fs.oss-cn-beijing.aliyuncs.com/updates/desktop';
const platform = process.argv[2];
const mode = process.argv[3] || 'release';
const arch = process.env.A_STOCK_DESKTOP_ARCH || process.arch;
if (!['mac', 'windows'].includes(platform) || !['release', 'dir'].includes(mode)) throw new Error('Usage: node electron-builder.mjs <mac|windows> <release|dir>');
if (!['arm64', 'x64'].includes(arch)) throw new Error(`Unsupported desktop architecture: ${arch}`);

const signingCertificate = resolveSigningCertificate(process.env.CSC_LINK);
if (process.env.CSC_LINK !== undefined && !signingCertificate) {
  // GitHub Actions exposes an empty/misconfigured certificate secret as a
  // relative workspace path in some environments. electron-builder then
  // attempts to import the project directory as a certificate. Remove only
  // invalid directory values so unsigned releases safely use ad-hoc signing.
  delete process.env.CSC_LINK;
  delete process.env.CSC_KEY_PASSWORD;
  console.warn('Ignoring empty or directory-valued CSC_LINK; using platform fallback signing.');
}

const outputDirectory = path.join(desktopRoot, 'dist', mode === 'dir' ? 'builder-dir' : 'builder-release');
const hasMacNotarizationCredentials = Boolean(
  signingCertificate && (
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
    'runtime-logger.cjs',
    'browser-auth.cjs',
    'data-protection.cjs',
    'hermes-runtime-root.cjs',
    'taoguba-browser-bridge.cjs',
    'update-feed.cjs',
    'update-manager.cjs',
    'user-data.cjs',
    'wechat-service.cjs',
    'xueqiu-browser-bridge.cjs',
    'stockdb-service.cjs',
    'quant-terminal-service.cjs',
    // main.cjs 在运行时通过 path.join(__dirname, 'assets', 'easy-stock.png') 读取窗口
    // 图标与 macOS Dock 图标。而 directories.buildResources 指向 desktop/assets，
    // electron-builder 默认会把 buildResources 整个排除在 app 之外，图标会静默丢失
    // （fs.existsSync 为假，不报错）。这里按运行时的实际需要精确纳入；
    // 其余 .icns/.ico/.iconset/.svg 是构建期资源，不进 asar。
    // desktop/test/packaging-assets.test.cjs 会校验这行与 main.cjs 的引用保持一致。
    'assets/easy-stock.png',
    'package.json',
    '!dist{,/**/*}',
    '!resources{,/**/*}',
    '!scripts{,/**/*}',
    '!test{,/**/*}',
  ],
  extraResources: [{ from: path.join(desktopRoot, 'resources'), to: 'resources' }],
  asar: true,
  publish: [{ provider: 'generic', url: updateFeedURL }],
  mac: {
    category: 'public.app-category.finance',
    icon: path.join(desktopRoot, 'assets', 'easy-stock.icns'),
    target: mode === 'dir' ? [{ target: 'dir', arch: [arch] }] : [{ target: 'zip', arch: [arch] }],
    artifactName: `easy-stock-v${packageManifest.version}-macos-${arch}.\${ext}`,
    hardenedRuntime: true,
    gatekeeperAssess: false,
    // Keep unsigned local builds launchable: electron-builder's `-` identity
    // creates a complete ad-hoc signature, while `null` leaves only the
    // Electron binary signed and produces an invalid app bundle on macOS.
    identity: signingCertificate ? undefined : '-',
    notarize: hasMacNotarizationCredentials,
  },
  dmg: { artifactName: `easy-stock-v${packageManifest.version}-macos-${arch}.dmg` },
  win: {
    icon: path.join(desktopRoot, 'assets', 'easy-stock.ico'),
    target: mode === 'dir' ? [{ target: 'dir', arch: [arch] }] : [{ target: 'nsis', arch: [arch] }],
    artifactName: `easy-stock-v${packageManifest.version}-windows-${arch}.\${ext}`,
    // 未配置签名证书时，只关掉「文件签名」，保留 resedit 资源编辑 —— exe 的图标、
    // 产品名/版本/版权元数据与执行级别都靠它写入。若改用更粗的开关把资源编辑一并
    // 关掉，打包出的 exe 会显示默认 Electron 图标且没有版本信息。
    ...(signingCertificate ? {} : { signExecutable: false }),
  },
  nsis: {
    oneClick: false,
    perMachine: false,
    allowElevation: true,
    allowToChangeInstallationDirectory: true,
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

function resolveSigningCertificate(value) {
  const certificate = value?.trim();
  if (!certificate) return '';
  const candidates = certificate.startsWith('file://')
    ? [fileURLToPath(certificate)]
    : path.isAbsolute(certificate)
      ? [certificate]
      : [
          path.resolve(process.cwd(), certificate),
          path.resolve(desktopRoot, certificate),
          path.resolve(desktopRoot, '..', certificate),
        ];
  if (candidates.some((candidate) => fs.existsSync(candidate) && fs.statSync(candidate).isDirectory())) return '';
  return certificate;
}
