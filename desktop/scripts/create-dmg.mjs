import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const arch = process.env.A_STOCK_DESKTOP_ARCH || process.arch;
if (!['arm64', 'x64'].includes(arch)) throw new Error(`Unsupported macOS architecture: ${arch}`);
const packageManifest = JSON.parse(fs.readFileSync(path.join(desktopRoot, 'package.json'), 'utf8'));
const appPathCandidates = [
	path.join(desktopRoot, 'dist', 'builder-release', arch === 'x64' ? 'mac' : `mac-${arch}`, 'easy-stock.app'),
	path.join(desktopRoot, 'dist', 'builder-release', `mac-${arch}`, 'easy-stock.app'),
	path.join(desktopRoot, 'dist', `easy-stock-darwin-${arch}`, 'easy-stock.app'),
];
const appPath = appPathCandidates.find((candidate) => fs.existsSync(candidate));
const releaseRoot = path.join(desktopRoot, 'dist', 'release');
const assetBase = `easy-stock-v${packageManifest.version}-macos-${arch}`;
const dmgPath = path.join(releaseRoot, `${assetBase}.dmg`);
const zipPath = path.join(releaseRoot, `${assetBase}.zip`);
if (!appPath) throw new Error(`Packaged app not found. Checked:\n${appPathCandidates.map((candidate) => `- ${candidate}`).join('\n')}`);
fs.mkdirSync(releaseRoot, { recursive: true });
fs.rmSync(dmgPath, { force: true });

const stagingRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-dmg-'));
const writableImagePath = path.join(stagingRoot, 'easy-stock-writable.dmg');
const writableMountPath = path.join(stagingRoot, 'writable-volume');
const verificationMountPath = path.join(stagingRoot, 'verification-volume');
let writableMounted = false;
let verificationMounted = false;
try {
	fs.mkdirSync(writableMountPath);
	fs.mkdirSync(verificationMountPath);
	// Both Node's fs.cpSync() and direct folder-image creation resolve macOS
	// framework symlinks to absolute paths. Create a writable volume first and
	// copy into the mounted filesystem with `ditto`, which preserves the
	// relative links required by Electron frameworks.
	run('hdiutil', ['create', '-size', '1200m', '-fs', 'Journaled HFS+', '-volname', 'easy-stock', '-ov', '-type', 'UDIF', writableImagePath]);
	run('hdiutil', ['attach', writableImagePath, '-nobrowse', '-mountpoint', writableMountPath]);
	writableMounted = true;
	const stagedAppPath = path.join(writableMountPath, 'easy-stock.app');
	run('ditto', [appPath, stagedAppPath]);
	run(process.execPath, [path.join(desktopRoot, 'scripts', 'verify-release-package.mjs'), stagedAppPath, 'macos']);
	fs.symlinkSync('/Applications', path.join(writableMountPath, 'Applications'));
	run('sync', []);
	detach(writableMountPath);
	writableMounted = false;
	run('hdiutil', ['convert', writableImagePath, '-format', 'UDZO', '-o', dmgPath]);
	run('hdiutil', ['verify', dmgPath]);
	run('hdiutil', ['attach', dmgPath, '-nobrowse', '-readonly', '-mountpoint', verificationMountPath]);
	verificationMounted = true;
	run(process.execPath, [path.join(desktopRoot, 'scripts', 'verify-release-package.mjs'), path.join(verificationMountPath, 'easy-stock.app'), 'macos']);
	detach(verificationMountPath);
	verificationMounted = false;
} finally {
	if (verificationMounted) tryRun('hdiutil', ['detach', verificationMountPath, '-force']);
	if (writableMounted) tryRun('hdiutil', ['detach', writableMountPath, '-force']);
	fs.rmSync(stagingRoot, { recursive: true, force: true });
}
if (!fs.existsSync(zipPath)) run('ditto', ['-c', '-k', '--sequesterRsrc', '--keepParent', appPath, zipPath]);
run(process.execPath, [path.join(desktopRoot, 'scripts', 'verify-release-package.mjs'), appPath, 'macos']);
console.log(`macOS release assets created:\n- ${dmgPath}\n- ${zipPath}`);

function run(command, args) {
	const result = spawnSync(command, args, { stdio: 'inherit' });
	if (result.error) throw result.error;
	if (result.status !== 0) throw new Error(`${command} exited with status ${result.status ?? 1}`);
}

function tryRun(command, args) {
	spawnSync(command, args, { stdio: 'ignore' });
}

function detach(mountPath) {
	const firstAttempt = spawnSync('hdiutil', ['detach', mountPath], { stdio: 'inherit' });
	if (firstAttempt.error) throw firstAttempt.error;
	if (firstAttempt.status === 0) return;
	// Spotlight/Finder can briefly keep a newly-created volume busy. Retry with
	// force after a short delay; this only detaches our temporary build volume.
	const forcedAttempt = spawnSync('hdiutil', ['detach', mountPath, '-force'], { stdio: 'inherit' });
	if (forcedAttempt.error) throw forcedAttempt.error;
	if (forcedAttempt.status !== 0) throw new Error(`hdiutil detach exited with status ${forcedAttempt.status ?? 1}`);
}
