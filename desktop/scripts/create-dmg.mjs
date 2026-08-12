import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const arch = process.env.A_STOCK_DESKTOP_ARCH || process.arch;
if (!['arm64', 'x64'].includes(arch)) throw new Error(`Unsupported macOS architecture: ${arch}`);
const packageManifest = JSON.parse(fs.readFileSync(path.join(desktopRoot, 'package.json'), 'utf8'));
const builderAppPath = path.join(desktopRoot, 'dist', 'builder-release', `mac-${arch}`, 'easy-stock.app');
const legacyAppPath = path.join(desktopRoot, 'dist', `easy-stock-darwin-${arch}`, 'easy-stock.app');
const appPath = fs.existsSync(builderAppPath) ? builderAppPath : legacyAppPath;
const releaseRoot = path.join(desktopRoot, 'dist', 'release');
const assetBase = `easy-stock-v${packageManifest.version}-macos-${arch}`;
const dmgPath = path.join(releaseRoot, `${assetBase}.dmg`);
const zipPath = path.join(releaseRoot, `${assetBase}.zip`);
if (!fs.existsSync(appPath)) throw new Error(`Packaged app not found: ${appPath}`);
fs.mkdirSync(releaseRoot, { recursive: true });
fs.rmSync(dmgPath, { force: true });

const stagingRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-dmg-'));
try {
	fs.cpSync(appPath, path.join(stagingRoot, 'easy-stock.app'), { recursive: true });
	fs.symlinkSync('/Applications', path.join(stagingRoot, 'Applications'));
	run('hdiutil', ['create', '-volname', 'easy-stock', '-srcfolder', stagingRoot, '-ov', '-format', 'UDZO', dmgPath]);
} finally {
	fs.rmSync(stagingRoot, { recursive: true, force: true });
}
if (!fs.existsSync(zipPath)) run('ditto', ['-c', '-k', '--sequesterRsrc', '--keepParent', appPath, zipPath]);
run(process.execPath, [path.join(desktopRoot, 'scripts', 'verify-release-package.mjs'), appPath, 'macos']);
console.log(`macOS release assets created:\n- ${dmgPath}\n- ${zipPath}`);

function run(command, args) {
	const result = spawnSync(command, args, { stdio: 'inherit' });
	if (result.error) throw result.error;
	if (result.status !== 0) process.exit(result.status ?? 1);
}
