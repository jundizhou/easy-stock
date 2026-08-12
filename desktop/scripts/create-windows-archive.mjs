import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const arch = process.env.A_STOCK_DESKTOP_ARCH || process.arch;
if (!['arm64', 'x64'].includes(arch)) throw new Error(`Unsupported Windows architecture: ${arch}`);
const packageManifest = JSON.parse(fs.readFileSync(path.join(desktopRoot, 'package.json'), 'utf8'));
const builderPackageRoot = path.join(desktopRoot, 'dist', 'builder-release', 'win-unpacked');
const legacyPackageRoot = path.join(desktopRoot, 'dist', `easy-stock-win32-${arch}`);
const packageRoot = fs.existsSync(builderPackageRoot) ? builderPackageRoot : legacyPackageRoot;
const releaseRoot = path.join(desktopRoot, 'dist', 'release');
const archivePath = path.join(releaseRoot, `easy-stock-v${packageManifest.version}-windows-${arch}.zip`);
if (!fs.existsSync(path.join(packageRoot, 'easy-stock.exe'))) throw new Error(`Packaged app not found: ${packageRoot}`);
fs.mkdirSync(releaseRoot, { recursive: true });
fs.rmSync(archivePath, { force: true });

run(process.execPath, [path.join(desktopRoot, 'scripts', 'verify-release-package.mjs'), packageRoot, 'windows']);
const result = spawnSync('powershell.exe', [
	'-NoLogo',
	'-NoProfile',
	'-NonInteractive',
	'-Command',
	"$ErrorActionPreference = 'Stop'; Compress-Archive -LiteralPath $env:EASY_STOCK_ARCHIVE_SOURCE -DestinationPath $env:EASY_STOCK_ARCHIVE_DESTINATION -CompressionLevel Optimal -Force",
], {
	stdio: 'inherit',
	env: {
		...process.env,
		EASY_STOCK_ARCHIVE_SOURCE: packageRoot,
		EASY_STOCK_ARCHIVE_DESTINATION: archivePath,
	},
});
if (result.error) throw result.error;
if (result.status !== 0) process.exit(result.status ?? 1);
console.log(`Windows release asset created: ${archivePath}`);

function run(command, args) {
	const result = spawnSync(command, args, { stdio: 'inherit' });
	if (result.error) throw result.error;
	if (result.status !== 0) process.exit(result.status ?? 1);
}
