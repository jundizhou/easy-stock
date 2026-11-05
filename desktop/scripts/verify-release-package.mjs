import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { verifyCodexRuntime } from './codex-runtime.mjs';
import { verifyHermesRuntime } from './verify-hermes-runtime.mjs';

const packageRoot = path.resolve(process.argv[2] || '');
const platform = process.argv[3];
if (!packageRoot || !['macos', 'windows'].includes(platform)) {
	throw new Error('Usage: node verify-release-package.mjs <package-root> <macos|windows>');
}
if (!fs.existsSync(packageRoot)) throw new Error(`Release package not found: ${packageRoot}`);

const resourcesRoot = platform === 'macos'
	? path.join(packageRoot, 'Contents', 'Resources', 'resources')
	: path.join(packageRoot, 'resources', 'resources');
const executable = platform === 'macos'
	? path.join(packageRoot, 'Contents', 'MacOS', 'easy-stock')
	: path.join(packageRoot, 'easy-stock.exe');
const backend = path.join(resourcesRoot, 'backend', platform === 'windows' ? 'easy-stock-backend.exe' : 'easy-stock-backend');
const runtimePython = platform === 'windows'
	? path.join(resourcesRoot, 'hermes-runtime', 'python', 'python.exe')
	: path.join(resourcesRoot, 'hermes-runtime', 'venv', 'bin', 'python');
const requiredPaths = [
	executable,
	backend,
	runtimePython,
	path.join(resourcesRoot, 'frontend', 'dist', 'index.html'),
	path.join(resourcesRoot, 'hermes-runtime', 'runtime-manifest.json'),
	path.join(resourcesRoot, 'agent-browser'),
];
for (const requiredPath of requiredPaths) {
	if (!fs.existsSync(requiredPath)) throw new Error(`Release package is incomplete: ${requiredPath}`);
}
if (fs.existsSync(path.join(resourcesRoot, 'wechat-download-api'))) {
	throw new Error('Release package must not include the removed WeChat download service');
}
if (platform === 'macos') verifyMacBundle(packageRoot);
if (platform === 'windows' && fs.existsSync(path.join(resourcesRoot, 'hermes-runtime', 'venv'))) {
	throw new Error('Windows release still contains the build-only Hermes venv');
}

const forbiddenComponents = new Set([
	'.runtime',
	'hermes-home',
	'codex-home',
	'browser-auth',
	'Local Storage',
	'Session Storage',
	'Partitions',
]);
const forbiddenFiles = new Set([
	'.credentials.json',
	'auth.json',
	'Cookies',
	'Cookies-journal',
	'id_rsa',
	'id_ed25519',
]);
const violations = [];
walk(packageRoot, (entryPath, entry) => {
	const name = entry.name;
	if (forbiddenComponents.has(name) || forbiddenFiles.has(name)) violations.push(path.relative(packageRoot, entryPath));
	if (name === '.env' || (name.startsWith('.env.') && name !== '.env.example')) violations.push(path.relative(packageRoot, entryPath));
	if (/\.(?:db|db-shm|db-wal|sqlite|sqlite3)$/i.test(name)) violations.push(path.relative(packageRoot, entryPath));
});
if (violations.length) {
	throw new Error(`Release package contains local or sensitive runtime files:\n${violations.map((item) => `- ${item}`).join('\n')}`);
}
await verifyCodexRuntime(path.join(resourcesRoot, 'codex-runtime'));
verifyHermesRuntime(path.join(resourcesRoot, 'hermes-runtime'));
console.log(`Release package verified: ${packageRoot}`);

function verifyMacBundle(appPath) {
	const invalidLinks = [];
	walk(appPath, (entryPath, entry) => {
		if (!entry.isSymbolicLink()) return;
		const target = fs.readlinkSync(entryPath);
		if (path.isAbsolute(target) || !fs.existsSync(entryPath)) {
			invalidLinks.push(`${path.relative(appPath, entryPath)} -> ${target}`);
		}
	});
	if (invalidLinks.length) {
		throw new Error(`macOS release contains invalid framework symlinks:\n${invalidLinks.map((item) => `- ${item}`).join('\n')}`);
	}
	const result = spawnSync('codesign', ['--verify', '--deep', '--strict', appPath], { encoding: 'utf8' });
	if (result.error) throw result.error;
	if (result.status !== 0) {
		throw new Error(`macOS release failed code-signature validation: ${(result.stderr || result.stdout || '').trim()}`);
	}
}

function walk(root, visit) {
	for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
		const entryPath = path.join(root, entry.name);
		visit(entryPath, entry);
		if (entry.isDirectory()) walk(entryPath, visit);
	}
}
