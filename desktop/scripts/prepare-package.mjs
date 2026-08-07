import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { prepareHermesRuntime } from './hermes-runtime.mjs';
import { prepareWechatRuntime } from './wechat-runtime.mjs';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const repoRoot = path.resolve(desktopRoot, '..');
const resourcesRoot = path.join(desktopRoot, 'resources');
const backendName = process.platform === 'win32' ? 'easy-stock-backend.exe' : 'easy-stock-backend';

fs.rmSync(resourcesRoot, { recursive: true, force: true });
fs.mkdirSync(path.join(resourcesRoot, 'backend'), { recursive: true });

run('npm', ['--workspace', 'frontend', 'run', 'build'], repoRoot);
run('go', ['build', '-trimpath', '-o', path.join(resourcesRoot, 'backend', backendName), './cmd/server'], path.join(repoRoot, 'backend'), { CGO_ENABLED: process.env.CGO_ENABLED || '0' });
fs.cpSync(path.join(repoRoot, 'frontend', 'dist'), path.join(resourcesRoot, 'frontend', 'dist'), { recursive: true });
prepareAgentBrowser();

const manifest = prepareHermesRuntime({ runtimeRoot: path.join(resourcesRoot, 'hermes-runtime') });
const wechatManifest = prepareWechatRuntime({
	resourcesRoot,
	runtimeRoot: path.join(resourcesRoot, 'hermes-runtime'),
});
console.log(`Desktop resources ready: Hermes ${manifest.version} (${manifest.mode}), WeChat API ${wechatManifest.revision.slice(0, 12)}`);

function prepareAgentBrowser() {
	const packageRoot = path.join(repoRoot, 'node_modules', 'agent-browser');
	const binaryName = process.platform === 'win32'
		? 'agent-browser-win32-x64.exe'
		: `agent-browser-${process.platform}-${process.arch}`;
	const sourceBinary = path.join(packageRoot, 'bin', binaryName);
	if (!fs.existsSync(sourceBinary)) throw new Error(`agent-browser binary not found: ${sourceBinary}`);
	const targetRoot = path.join(resourcesRoot, 'agent-browser');
	fs.mkdirSync(targetRoot, { recursive: true });
	const wrapperName = process.platform === 'win32' ? 'agent-browser.py' : 'agent-browser';
	fs.copyFileSync(path.join(desktopRoot, 'scripts', 'browser-bin', 'agent-browser'), path.join(targetRoot, wrapperName));
	fs.copyFileSync(sourceBinary, path.join(targetRoot, process.platform === 'win32' ? 'agent-browser-real.exe' : 'agent-browser-real'));
	if (process.platform !== 'win32') {
		fs.chmodSync(path.join(targetRoot, wrapperName), 0o755);
		fs.chmodSync(path.join(targetRoot, 'agent-browser-real'), 0o755);
	}
}

function run(command, args, cwd, env = {}) {
	const result = spawnSync(command, args, { cwd, stdio: 'inherit', env: { ...process.env, ...env } });
	if (result.error) throw result.error;
	if (result.status !== 0) process.exit(result.status ?? 1);
}
