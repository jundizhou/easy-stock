import './builder-filesystem.mjs';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import updateFeed from '../update-feed.cjs';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const packageManifest = JSON.parse(fs.readFileSync(path.join(desktopRoot, 'package.json'), 'utf8'));
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
    'startup.cjs',
    'runtime-logger.cjs',
    'browser-auth.cjs',
    'data-protection.cjs',
    'eastmoney-push2-bridge.cjs',
    'eastmoney-session-bridge.cjs',
    'hermes-runtime-root.cjs',
    'taoguba-browser-bridge.cjs',
    'update-feed.cjs',
    'update-manager.cjs',
    'user-data.cjs',
    'xueqiu-browser-bridge.cjs',
    'package.json',
    '!dist{,/**/*}',
    '!resources{,/**/*}',
    '!scripts{,/**/*}',
    '!test{,/**/*}',
  ],
  extraResources: [{ from: path.join(desktopRoot, 'resources'), to: 'resources' }],
  asar: true,
  publish: [updateFeed.resolveUpdateFeed()],
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
    // Unsigned local builds still need rcedit, so the app executable carries the
    // easy-stock icon and version metadata. `signAndEditExecutable: false` skips
    // both, which leaves Electron's default icon on the executable and on every
    // shortcut the installer creates.
    ...(signingCertificate ? {} : { signExecutable: false }),
  },
  nsis: {
    include: path.join(desktopRoot, 'assets', 'installer.nsh'),
    // Extract directly into the install directory; the 7z path first writes
    // every runtime file to a temporary directory and then copies it again.
    useZip: true,
    differentialPackage: false,
    oneClick: false,
    perMachine: false,
    allowElevation: true,
    allowToChangeInstallationDirectory: true,
    deleteAppDataOnUninstall: false,
    artifactName: `easy-stock-v${packageManifest.version}-windows-${arch}-setup.exe`,
  },
};

const { Arch, build, Platform } = await import('electron-builder');

if (platform === 'windows' && mode === 'release') {
  // Use the builder's checksum-verified Windows toolset on every build host.
  // The standalone extractor supports long paths without the old NSIS ZIP plugin.
  const { downloadBuilderToolset } = await import('app-builder-lib/out/util/electronGet.js');
  const extractorRoot = await downloadBuilderToolset({
    releaseName: '7zip@1.0.0',
    filenameWithExt: '7zip-win-x64.tar.gz',
    checksums: { '7zip-win-x64.tar.gz': 'be071f15bd6da2f78fe81c6ddef2009b0c4d8a51f36b780cb806c7e6df95e1b3' },
    githubOrgRepo: 'electron-userland/electron-builder-binaries',
  });
  const nsisPath = (value) => value.replace(/\$/g, () => '$$').replace(/"/g, () => '$\\"');
  const originalInclude = config.nsis.include;
  const hook = fs.readFileSync(originalInclude, 'utf8');
  const hookStart = '!macro customCheckAppRunning';
  if (hook.indexOf(hookStart) < 0 || hook.indexOf(hookStart) !== hook.lastIndexOf(hookStart)
    || (hook.match(/!macroend/g) || []).length !== 1) {
    throw new Error('Expected one customCheckAppRunning installer hook');
  }
  const override = path.join(outputDirectory, 'easy-stock-direct-extract.nsh');
  fs.writeFileSync(override, String.raw`
; NSIS otherwise measures writing the archive, not extracting its contents.
; Show activity throughout installation instead of a misleading repeated percentage.
!macro easyStockStartInstallProgress
  Var /GLOBAL easyStockProgressStyle
  IfSilent easyStockProgressStarted
    Push $0
    System::Call 'user32::GetWindowLongW(p $mui.InstFilesPage.ProgressBar, i -16) i.r0'
    StrCpy $easyStockProgressStyle $0
    IntOp $0 $0 | 0x08
    System::Call 'user32::SetWindowLongW(p $mui.InstFilesPage.ProgressBar, i -16, i r0)'
    SendMessage $mui.InstFilesPage.ProgressBar 0x40A 1 30
    Pop $0
  easyStockProgressStarted:
!macroend

!macro easyStockFinishInstallProgress
  IfSilent easyStockProgressFinished
    SendMessage $mui.InstFilesPage.ProgressBar 0x40A 0 0
    System::Call 'user32::SetWindowLongW(p $mui.InstFilesPage.ProgressBar, i -16, i $easyStockProgressStyle)'
  easyStockProgressFinished:
!macroend

!macro easyStockPrepareUpgradeTemp
  Var /GLOBAL easyStockUpgradeTempDir
  Var /GLOBAL easyStockPreviousTemp
  Var /GLOBAL easyStockPreviousTmp
  Push $0
  Push $R7
  Push $R8
  Push $R9
  StrCpy $easyStockUpgradeTempDir ""
  StrCpy $R8 "$INSTDIR" 3
  StrCpy $R7 "$R8" "" 1
  StrCmp $R7 ":\" 0 easyStockTempDone
  ClearErrors
  GetTempFileName $R9 "$R8"
  IfErrors easyStockTempDone
  Delete "$R9"
  ClearErrors
  CreateDirectory "$R9"
  IfErrors easyStockTempDone
  StrCpy $easyStockUpgradeTempDir "$R9"
  ReadEnvStr $easyStockPreviousTemp "TEMP"
  ReadEnvStr $easyStockPreviousTmp "TMP"
  System::Call 'kernel32::SetEnvironmentVariableW(w "TEMP", w "$R9") i.r0'
  System::Call 'kernel32::SetEnvironmentVariableW(w "TMP", w "$R9") i.r0'
  easyStockTempDone:
  Pop $R9
  Pop $R8
  Pop $R7
  Pop $0
!macroend

!macro easyStockRestoreUpgradeTemp
  Push $0
  StrCmp $easyStockUpgradeTempDir "" +5
    System::Call 'kernel32::SetEnvironmentVariableW(w "TEMP", w "$easyStockPreviousTemp") i.r0'
    System::Call 'kernel32::SetEnvironmentVariableW(w "TMP", w "$easyStockPreviousTmp") i.r0'
    RMDir /r "$easyStockUpgradeTempDir"
    StrCpy $easyStockUpgradeTempDir ""
  Pop $0
!macroend

!macroundef decompress
!macro decompress
  !insertmacro easyStockStartInstallProgress
  File /oname=$PLUGINSDIR\easy-stock-7za.exe "${nsisPath(path.join(extractorRoot, 'bin', '7za.exe'))}"
  DetailPrint "$(installing)"
  nsExec::ExecToLog '"$PLUGINSDIR\easy-stock-7za.exe" x "$PLUGINSDIR\app-$packageArch.zip" "-o$INSTDIR" -y -bso0 -bsp0'
  Pop $R0
  StrCmp $R0 "0" easyStockExtractionDone
    MessageBox MB_OK|MB_ICONEXCLAMATION "$(decompressionFailed)$\n$R0" /SD IDOK
    !insertmacro easyStockRestoreUpgradeTemp
    SetErrorLevel 2
    Quit
  easyStockExtractionDone:
  SetOutPath "$INSTDIR\resources\installer-tools"
  File /oname=7zip-LICENSE.txt "${nsisPath(path.join(extractorRoot, 'LICENSE.txt'))}"
  File /oname=7zip-COPYING.txt "${nsisPath(path.join(extractorRoot, 'COPYING'))}"
  SetOutPath "$INSTDIR"
!macroend

!macro customInstall
  !insertmacro easyStockRestoreUpgradeTemp
  !insertmacro easyStockFinishInstallProgress
!macroend
`);
  const escapedOverride = nsisPath(override);
  config.nsis.include = path.join(outputDirectory, 'easy-stock-installer.nsh');
  fs.writeFileSync(config.nsis.include, hook.replace(hookStart,
    () => `${hookStart}\n!ifndef BUILD_UNINSTALLER\n!include "${escapedOverride}"\n!endif`)
    .replace('!macroend', '!ifndef BUILD_UNINSTALLER\n!insertmacro easyStockPrepareUpgradeTemp\n!endif\n!macroend'));
}

const targetPlatform = platform === 'mac' ? Platform.MAC : Platform.WINDOWS;
const targetArch = Arch[arch];
const artifacts = await build({
  targets: mode === 'dir'
    ? targetPlatform.createTarget(['dir'], targetArch)
    : targetPlatform.createTarget(platform === 'mac' ? ['zip'] : ['nsis'], targetArch),
  config,
  projectDir: desktopRoot,
  publish: 'never',
});

if (platform === 'windows' && mode === 'release') {
  // ZIP payloads bypass electron-builder's automatic blockmap generation.
  // Keep external installer blockmaps for the existing updater and checks.
  const { buildBlockMap } = await import('app-builder-lib/out/targets/blockmap/blockmap.js');
  for (const artifact of artifacts.filter((file) => file.endsWith('-setup.exe'))) {
    await buildBlockMap(artifact, 'gzip', `${artifact}.blockmap`);
  }
}

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
