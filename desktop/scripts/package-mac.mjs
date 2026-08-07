import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { packager } from '@electron/packager';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const out = path.join(desktopRoot, 'dist');
const options = {
	dir: desktopRoot,
	name: 'easy-stock',
	platform: 'darwin',
	arch: process.env.A_STOCK_DESKTOP_ARCH || process.arch,
	out,
	overwrite: true,
	appBundleId: 'com.jundizhou.easystock',
	appVersion: '0.1.0',
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
};
const icon = path.join(desktopRoot, 'assets', 'easy-stock.icns');
if (fs.existsSync(icon)) options.icon = icon;

await packager(options);
console.log(`macOS app created in ${out}`);
