import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const arch = process.env.A_STOCK_DESKTOP_ARCH || process.arch;
const appPath = path.join(desktopRoot, 'dist', `easy-stock-darwin-${arch}`, 'easy-stock.app');
const dmgPath = path.join(desktopRoot, 'dist', `easy-stock-${arch}.dmg`);
if (!fs.existsSync(appPath)) throw new Error(`Packaged app not found: ${appPath}`);
fs.rmSync(dmgPath, { force: true });
const result = spawnSync('hdiutil', ['create', '-volname', 'easy-stock', '-srcfolder', appPath, '-ov', '-format', 'UDZO', dmgPath], { stdio: 'inherit' });
if (result.error) throw result.error;
if (result.status !== 0) process.exit(result.status ?? 1);
console.log(`macOS installer created: ${dmgPath}`);
