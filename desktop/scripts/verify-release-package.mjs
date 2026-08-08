import fs from 'node:fs';
import path from 'node:path';

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
const requiredPaths = [
	executable,
	backend,
	path.join(resourcesRoot, 'frontend', 'dist', 'index.html'),
	path.join(resourcesRoot, 'hermes-runtime', 'runtime-manifest.json'),
	path.join(resourcesRoot, 'wechat-download-api', 'easy-stock-wechat-runtime.json'),
	path.join(resourcesRoot, 'agent-browser'),
];
for (const requiredPath of requiredPaths) {
	if (!fs.existsSync(requiredPath)) throw new Error(`Release package is incomplete: ${requiredPath}`);
}

const forbiddenComponents = new Set([
	'.runtime',
	'hermes-home',
	'browser-auth',
	'Local Storage',
	'Session Storage',
	'Partitions',
]);
const forbiddenFiles = new Set([
	'.credentials.json',
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
console.log(`Release package verified: ${packageRoot}`);

function walk(root, visit) {
	for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
		const entryPath = path.join(root, entry.name);
		visit(entryPath, entry);
		if (entry.isDirectory()) walk(entryPath, visit);
	}
}
