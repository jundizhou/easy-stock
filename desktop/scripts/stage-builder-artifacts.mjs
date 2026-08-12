import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const platform = process.argv[2];
if (!['mac', 'windows'].includes(platform)) throw new Error('Usage: node stage-builder-artifacts.mjs <mac|windows>');
const sourceRoot = path.join(desktopRoot, 'dist', 'builder-release');
const releaseRoot = path.join(desktopRoot, 'dist', 'release');
fs.mkdirSync(releaseRoot, { recursive: true });
for (const entry of fs.readdirSync(releaseRoot)) fs.rmSync(path.join(releaseRoot, entry), { recursive: true, force: true });

const allowed = platform === 'mac'
  ? (name) => name === 'latest-mac.yml' || name.endsWith('.zip') || name.endsWith('.zip.blockmap')
  : (name) => name === 'latest.yml' || name.endsWith('.exe') || name.endsWith('.exe.blockmap');
const staged = [];
for (const entry of fs.readdirSync(sourceRoot, { withFileTypes: true })) {
  if (!entry.isFile() || !allowed(entry.name)) continue;
  fs.copyFileSync(path.join(sourceRoot, entry.name), path.join(releaseRoot, entry.name));
  staged.push(entry.name);
}
if (!staged.some((name) => platform === 'mac' ? name === 'latest-mac.yml' : name === 'latest.yml')) {
  throw new Error(`Updater metadata was not generated for ${platform}`);
}
console.log(`Staged ${platform} release artifacts:\n${staged.map((name) => `- ${name}`).join('\n')}`);
