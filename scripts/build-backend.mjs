import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const binaryName = process.platform === 'win32' ? 'easy-stock-backend.exe' : 'easy-stock-backend';
const outputPath = path.join(repoRoot, 'desktop', 'bin', binaryName);

fs.mkdirSync(path.dirname(outputPath), { recursive: true });

const build = spawnSync('go', ['build', '-o', outputPath, './cmd/server'], {
	cwd: path.join(repoRoot, 'backend'),
	stdio: 'inherit',
	shell: process.platform === 'win32',
});

if (build.error) {
	console.error(`backend build failed: ${build.error.message}`);
	process.exit(1);
}

process.exit(build.status ?? 1);
