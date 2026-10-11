/**
 * 「唯一版本」一致性校验内核。
 *
 * 规范来源：本机只允许存在一个运行版本的 easy-stock。
 * 链条：源码 → desktop/bin（后端） / frontend/dist（前端） → desktop/resources → win-unpacked → 已安装 App。
 * 任何一步漏同步都会造成「新壳旧核」，因此这里把四处副本逐项比对，供 CLI 与单元测试共用。
 */
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { createReadStream } from 'node:fs';
import { fileURLToPath } from 'node:url';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const repoRoot = path.resolve(desktopRoot, '..');

export const BACKEND_NAME = process.platform === 'win32' ? 'easy-stock-backend.exe' : 'easy-stock-backend';

export function installedDir() {
	const override = (process.env.EASY_STOCK_INSTALLED_DIR || '').trim();
	if (override) return override;
	const localAppData = process.env.LOCALAPPDATA || '';
	return localAppData ? path.join(localAppData, 'Programs', 'easy-stock') : '';
}

export function unpackedDir() {
	return path.join(desktopRoot, 'dist', 'builder-dir', 'win-unpacked');
}

/**
 * 后端二进制文件名在两条链路上不一致（build-backend 产出无扩展名，打包资源里带 .exe），
 * 这里统一容错解析，避免「明明同步了却判定缺失」。
 */
export function resolveBackendFile(dir) {
	const candidates = process.platform === 'win32'
		? ['easy-stock-backend.exe', 'easy-stock-backend']
		: ['easy-stock-backend', 'easy-stock-backend.exe'];
	for (const name of candidates) {
		const candidate = path.join(dir, name);
		if (fs.existsSync(candidate)) return candidate;
	}
	return path.join(dir, candidates[0]);
}

/** 四处后端二进制（唯一真源是第 ① 项）。 */
export function backendCopies() {
	return [
		{ id: 'bin', label: '① bin（构建产物）', path: resolveBackendFile(path.join(desktopRoot, 'bin')) },
		{ id: 'resources', label: '② resources（打包资源）', path: resolveBackendFile(path.join(desktopRoot, 'resources', 'backend')) },
		{ id: 'unpacked', label: '③ win-unpacked（打包产物）', path: resolveBackendFile(path.join(unpackedDir(), 'resources', 'resources', 'backend')) },
		{ id: 'installed', label: '④ 已安装 App', path: resolveBackendFile(path.join(installedDir(), 'resources', 'resources', 'backend')) },
	];
}

function sha256File(target) {
	return new Promise((resolve) => {
		try {
			const hash = crypto.createHash('sha256');
			const stream = createReadStream(target);
			stream.on('error', () => resolve(null));
			stream.on('data', (chunk) => hash.update(chunk));
			stream.on('end', () => resolve(hash.digest('hex').slice(0, 16)));
		} catch {
			resolve(null);
		}
	});
}

/** 在二进制里找字节串（流式扫描，命中即停）。 */
function bufferIncludes(target, pattern) {
	return new Promise((resolve) => {
		try {
			let tail = Buffer.alloc(0);
			const stream = createReadStream(target, { highWaterMark: 1024 * 1024 });
			stream.on('error', () => resolve(false));
			stream.on('data', (chunk) => {
				const merged = tail.length ? Buffer.concat([tail, chunk]) : chunk;
				if (merged.includes(pattern)) {
					stream.destroy();
					resolve(true);
					return;
				}
				tail = merged.subarray(Math.max(0, merged.length - pattern.length + 1));
			});
			stream.on('end', () => resolve(false));
		} catch {
			resolve(false);
		}
	});
}

/** 在二进制里找 UTF-8 字符串。 */
function binaryIncludes(target, needle) {
	return bufferIncludes(target, Buffer.from(needle, 'utf8'));
}

/** 在 exe 里找 UTF-16LE 字符串（VS_VERSION_INFO 资源）。 */
function exeResourceIncludes(target, needle) {
	return bufferIncludes(target, Buffer.from(needle, 'utf16le'));
}

function currentVersion() {
	try {
		return JSON.parse(fs.readFileSync(path.join(desktopRoot, 'package.json'), 'utf8')).version;
	} catch {
		return '';
	}
}

function indexAssets(dir) {
	try {
		return fs.readdirSync(dir).filter((name) => /^index-.*\.(js|css)$/.test(name)).sort();
	} catch {
		return null;
	}
}

/**
 * 收集所有校验项。
 * @param {{ skipInstalled?: boolean, deep?: boolean }} options
 *   skipInstalled：装机副本不存在时跳过（CI 场景）
 *   deep：是否扫描 exe 内部资源与后端能力串（较慢，单元测试里关闭）
 */
export async function collectChecks(options = {}) {
	const { skipInstalled = false, deep = true } = options;
	const version = currentVersion();
	const unpacked = unpackedDir();
	const installed = installedDir();
	const installedPresent = Boolean(installed) && fs.existsSync(path.join(installed, 'easy-stock.exe'));
	const useInstalled = installedPresent && !skipInstalled;

	const rows = [];
	const push = (name, ok, detail) => rows.push({ name, ok: Boolean(ok), detail: String(detail ?? '') });

	// 1) 后端二进制：所有存在的副本必须与 ① 完全一致
	const copies = backendCopies().filter((entry) => entry.id !== 'installed' || useInstalled);
	const shas = {};
	for (const entry of copies) shas[entry.id] = await sha256File(entry.path);
	// 基线优先级：desktop/bin > desktop/resources > win-unpacked（CI 走 prepare-package 时没有 bin）
	const baseId = ['bin', 'resources', 'unpacked'].find((id) => shas[id]);
	const base = baseId ? shas[baseId] : null;
	const anyBackend = Object.values(shas).some(Boolean);
	if (!base) {
		push('后端 sha', !anyBackend, anyBackend ? '缺失基线：请先 npm run build:backend' : '跳过（尚未构建后端）');
	} else {
		for (const entry of copies) {
			if (entry.id === baseId) push(`后端 sha ${entry.label}（基线）`, true, shas[entry.id]);
			else push(`后端 sha ${entry.label}`, shas[entry.id] === base, shas[entry.id] ?? '缺失');
		}
	}

	// 2) 主程序与 asar
	const unpackedExe = path.join(unpacked, 'easy-stock.exe');
	const installedExe = path.join(installed, 'easy-stock.exe');
	if (fs.existsSync(unpackedExe) && (useInstalled || fs.existsSync(installedExe))) {
		const [a, b] = [await sha256File(unpackedExe), await sha256File(installedExe)];
		push('exe sha ③ vs ④', a && a === b, a ?? '缺失');
	}
	const unpackedAsar = path.join(unpacked, 'resources', 'app.asar');
	const installedAsar = path.join(installed, 'resources', 'app.asar');
	if (fs.existsSync(unpackedAsar) && (useInstalled || fs.existsSync(installedAsar))) {
		const [a, b] = [await sha256File(unpackedAsar), await sha256File(installedAsar)];
		push('asar sha ③ vs ④', a && a === b, a ?? '缺失');
	}

	if (deep) {
		// 3) exe 版本元数据（曾因 signAndEditExecutable:false 退化成 Electron 默认值）
		for (const [tag, file] of [['③', unpackedExe], ['④', installedExe]]) {
			if (!fs.existsSync(file)) continue;
			const [hasVersion, hasVendor] = [
				await exeResourceIncludes(file, version),
				await exeResourceIncludes(file, 'easy-stock contributors'),
			];
			push(`exe 元数据 ${tag} = ${version}`, hasVersion, hasVersion ? '✓' : '✗（疑似 Electron 默认值 38.8.6）');
			push(`exe 厂商 ${tag} = easy-stock contributors`, hasVendor, hasVendor ? '✓' : '✗');
		}
		// 4) 后端能力（执行网关）
		const be = resolveBackendFile(path.join(installedPresent ? installed : unpacked, 'resources', 'resources', 'backend'));
		if (fs.existsSync(be)) {
			const [route, kill] = [
				await binaryIncludes(be, '/api/v1/execution/target'),
				await binaryIncludes(be, 'A_STOCK_EXEC_KILLSWITCH'),
			];
			push('后端含 /api/v1/execution/target', route, route ? '✓' : '✗');
			push('后端含 A_STOCK_EXEC_KILLSWITCH', kill, kill ? '✓' : '✗');
		}
	}

	// 5) 前端 bundle：打包资源里必须只有一代，且与装机版一致
	const srcAssets = indexAssets(path.join(desktopRoot, 'resources', 'frontend', 'dist', 'assets'));
	if (!srcAssets) {
		push('前端 bundle 仅一代（打包资源）', true, '跳过（尚未构建前端）');
	} else {
		push('前端 bundle 仅一代（打包资源）', srcAssets.length === 2, srcAssets.join(', '));
	}
	if (useInstalled) {
		const instAssets = indexAssets(path.join(installed, 'resources', 'resources', 'frontend', 'dist', 'assets'));
		push('前端 bundle ③/④ 一致', (srcAssets || []).join('|') === (instAssets || []).join('|'), (instAssets || []).join(', ') || '缺失');
	}

	return { version, rows, installedPresent, usedInstalled: useInstalled };
}
