import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { prepareCodexRuntime, codexExecutable } from './codex-runtime.mjs';
import { prepareHermesRuntime, hermesRuntimePython } from './hermes-runtime.mjs';
import { prepareWechatRuntime } from './wechat-runtime.mjs';
import { resolveSidecarSources, stageSidecarAssets } from './sidecar-assets.mjs';
import { signSidecars } from './sign-sidecars.mjs';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const repoRoot = path.resolve(desktopRoot, '..');
const resourcesRoot = path.join(desktopRoot, 'resources');
const backendName = process.platform === 'win32' ? 'easy-stock-backend.exe' : 'easy-stock-backend';
const targetArch = process.env.A_STOCK_DESKTOP_ARCH || process.arch;

// 解析用于注入后端二进制的 git 提交号（-X main.buildCommit）。
// 优先取 A_STOCK_BUILD_COMMIT，其次本地 git；两者都不可用时返回空串（不注入）。
function resolveBuildCommit() {
	const fromEnv = (process.env.A_STOCK_BUILD_COMMIT || '').trim();
	if (fromEnv) {
		return fromEnv;
	}
	const probe = spawnSync('git', ['rev-parse', '--short', 'HEAD'], { cwd: repoRoot, encoding: 'utf8' });
	const sha = (probe.stdout || '').trim();
	return probe.status === 0 ? sha : '';
}

const buildCommit = resolveBuildCommit();

if (targetArch !== process.arch) {
	throw new Error(`Desktop target architecture ${targetArch} must match runner architecture ${process.arch} because native runtimes are bundled`);
}

fs.rmSync(resourcesRoot, { recursive: true, force: true });
fs.mkdirSync(path.join(resourcesRoot, 'backend'), { recursive: true });

runNpm(['--workspace', 'frontend', 'run', 'build'], repoRoot);
const backendBuildArgs = ['build', '-trimpath'];
if (buildCommit) {
	backendBuildArgs.push('-ldflags', `-X main.buildCommit=${buildCommit}`);
}
backendBuildArgs.push('-o', path.join(resourcesRoot, 'backend', backendName), './cmd/server');
run('go', backendBuildArgs, path.join(repoRoot, 'backend'), { CGO_ENABLED: process.env.CGO_ENABLED || '0' });
console.log(`Backend binary buildCommit=${buildCommit || '(unset)'}`);
fs.cpSync(path.join(repoRoot, 'frontend', 'dist'), path.join(resourcesRoot, 'frontend', 'dist'), { recursive: true });
// [p5-sidecar] 装载 Free-StockDB 引擎与 Quant Terminal（缺失仅告警，不阻塞上游打包）
const sidecarSources = resolveSidecarSources({
  env: process.env,
  platform: process.platform,
  defaults: {
    stockdbBin: process.env.A_STOCK_STOCKDB_SRC || 'C:/quant/tools/free-stockdb/stockdb/stockdb.exe',
    stockdbData: process.env.A_STOCK_STOCKDB_DATA_SRC || '',
    quantScript: process.env.A_STOCK_QUANT_SRC || 'C:/quant/quant_terminal.py',
  },
});
const sidecarManifest = stageSidecarAssets({ sources: sidecarSources, resourcesRoot });
signSidecars({ resourcesRoot, env: process.env });

prepareAgentBrowser();

const codexManifest = await prepareCodexRuntime({ runtimeRoot: path.join(resourcesRoot, 'codex-runtime') });
const manifest = prepareHermesRuntime({ runtimeRoot: path.join(resourcesRoot, 'hermes-runtime') });
const wechatManifest = prepareWechatRuntime({
	resourcesRoot,
	runtimeRoot: path.join(resourcesRoot, 'hermes-runtime'),
});
run('go', ['test', './internal/agent', '-run', 'TestCodexNative|TestBuiltinMCP', '-count=1'], path.join(repoRoot, 'backend'), {
    EASY_STOCK_CODEX_TEST_BINARY: codexExecutable(path.join(resourcesRoot, 'codex-runtime')),
    EASY_STOCK_HERMES_TEST_PYTHON: hermesRuntimePython(path.join(resourcesRoot, 'hermes-runtime')),
});
console.log(`Desktop resources ready: Codex ${codexManifest.version}, Hermes ${manifest.version} (${manifest.mode}), WeChat API ${wechatManifest.revision.slice(0, 12)}`);

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

function runNpm(args, cwd) {
	const npmExecPath = process.env.npm_execpath;
	if (!npmExecPath) throw new Error('npm_execpath is unavailable; run desktop packaging through an npm script');
	run(process.execPath, [npmExecPath, ...args], cwd);
}
