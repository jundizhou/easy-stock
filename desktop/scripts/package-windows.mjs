import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { packager } from '@electron/packager';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const out = path.join(desktopRoot, 'dist');
const packageManifest = JSON.parse(fs.readFileSync(path.join(desktopRoot, 'package.json'), 'utf8'));
const arch = process.env.A_STOCK_DESKTOP_ARCH || process.arch;
if (!['arm64', 'x64'].includes(arch)) throw new Error(`Unsupported Windows architecture: ${arch}`);

const options = {
	dir: desktopRoot,
	name: 'easy-stock',
	platform: 'win32',
	arch,
	out,
	overwrite: true,
	appVersion: packageManifest.version,
	download: {
		mirrorOptions: {
			mirror: process.env.ELECTRON_MIRROR || 'https://npmmirror.com/mirrors/electron/',
		},
	},
	derefSymlinks: false,
	extraResource: [path.join(desktopRoot, 'resources')],
	ignore: [
		/^\/dist($|\/)/,
		/^\/resources($|\/)/,
		/^\/scripts($|\/)/,
		/^\/test($|\/)/,
	],
	win32metadata: {
		CompanyName: 'easy-stock',
		FileDescription: 'easy-stock A-share research desktop app',
		InternalName: 'easy-stock',
		OriginalFilename: 'easy-stock.exe',
		ProductName: 'easy-stock',
	},
};
const icon = path.join(desktopRoot, 'assets', 'easy-stock.ico');
if (fs.existsSync(icon)) options.icon = icon;

await packager(options);
console.log(`Windows ${arch} app created in ${out}`);
