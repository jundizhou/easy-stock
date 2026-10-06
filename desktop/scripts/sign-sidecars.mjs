import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

// P5：sidecar 二进制补签（electron-builder 只签 Electron 壳与安装包，
// extraResources 里的 EXE 需单独用 signtool 签名，否则更新校验链不完整）。
//
// 启用条件：设置 A_STOCK_SIDECAR_SIGN_PFX（PFX 证书路径，密码用
// A_STOCK_SIDECAR_SIGN_PFX_PASSWORD）。未配置时安静跳过，不阻塞打包。

const SIGNTOOL_SEARCH_ROOTS = [
	'C:/Program Files (x86)/Windows Kits/10/bin',
	'C:/Program Files/Windows Kits/10/bin',
];

export function findSigntool(env = {}) {
	const configured = String(env.A_STOCK_SIGNTOOL || '').trim();
	if (configured) return configured;
	for (const root of SIGNTOOL_SEARCH_ROOTS) {
		if (!fs.existsSync(root)) continue;
		const versions = fs.readdirSync(root).filter((entry) => /^\d+\.\d+\.\d+\.\d+$/.test(entry)).sort().reverse();
		for (const version of versions) {
			const candidate = path.join(root, version, 'x64', 'signtool.exe');
			if (fs.existsSync(candidate)) return candidate;
		}
	}
	return 'signtool';
}

export function sidecarBinaries({ resourcesRoot, platform = process.platform } = {}) {
	const candidates = [
		path.join(resourcesRoot, 'backend', platform === 'win32' ? 'easy-stock-backend.exe' : 'easy-stock-backend'),
		path.join(resourcesRoot, 'stockdb', platform === 'win32' ? 'stockdb.exe' : 'stockdb'),
	];
	return candidates.filter((candidate) => fs.existsSync(candidate));
}

export function signSidecars({ resourcesRoot, env = {}, platform = process.platform, logger = console, runner } = {}) {
	const pfx = String(env.A_STOCK_SIDECAR_SIGN_PFX || '').trim();
	const files = sidecarBinaries({ resourcesRoot, platform });
	if (!files.length) return { signed: [], skipped: 'no sidecar binaries staged' };
	if (!pfx) {
		logger.log('[sidecar-sign] A_STOCK_SIDECAR_SIGN_PFX 未配置，跳过 sidecar 签名（安装包签名不受影响）');
		return { signed: [], skipped: 'signing certificate not configured' };
	}
	const signtool = findSigntool(env);
	const password = String(env.A_STOCK_SIDECAR_SIGN_PFX_PASSWORD || '');
	const args = ['sign', '/fd', 'SHA256', '/f', pfx];
	if (password) args.push('/p', password);
	const run = runner || ((toolArgs) => spawnSync(signtool, toolArgs, { stdio: 'pipe' }));
	const signed = [];
	for (const file of files) {
		const result = run([...args, file]);
		const failed = result.error || (result.status !== undefined && result.status !== 0);
		if (failed) {
			const detail = result.error?.message || String(result.stderr || result.output || '').trim();
			throw new Error(`[sidecar-sign] signtool 签名失败 (${file}): ${detail}`);
		}
		signed.push(file);
		logger.log(`[sidecar-sign] signed: ${file}`);
	}
	return { signed, skipped: '' };
}
