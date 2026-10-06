import fs from 'node:fs';
import path from 'node:path';

// P5：sidecar 资产装载（Free-StockDB 引擎 + Quant Terminal 脚本）。
// 策略：来源可由环境变量覆盖；来源缺失时警告并跳过（sidecar 为可选集成，
// 不阻塞纯上游功能的打包）。
export function resolveSidecarSources({ env = {}, platform = process.platform, defaults = {} } = {}) {
	const stockdbBinName = platform === 'win32' ? 'stockdb.exe' : 'stockdb';
	const pick = (value, fallback) => {
		const trimmed = String(value || '').trim();
		return trimmed || fallback;
	};
	return {
		stockdb: {
			binName: stockdbBinName,
			bin: pick(env.A_STOCK_STOCKDB_SRC, defaults.stockdbBin || ''),
			data: pick(env.A_STOCK_STOCKDB_DATA_SRC, defaults.stockdbData || ''),
		},
		quant: {
			script: pick(env.A_STOCK_QUANT_SRC, defaults.quantScript || ''),
		},
	};
}

function copyTree(from, to) {
	fs.cpSync(from, to, { recursive: true });
	return directoryBytes(to);
}

function directoryBytes(dir) {
	let total = 0;
	const stack = [dir];
	while (stack.length) {
		const current = stack.pop();
		let entries;
		try {
			entries = fs.readdirSync(current, { withFileTypes: true });
		} catch {
			continue;
		}
		for (const entry of entries) {
			const full = path.join(current, entry.name);
			if (entry.isDirectory()) stack.push(full);
			else if (entry.isFile()) {
				try { total += fs.statSync(full).size; } catch { /* ignore */ }
			}
		}
	}
	return total;
}

/**
 * 装载 sidecar 资产到 resources：
 *   resources/stockdb/stockdb(.exe)   引擎二进制
 *   resources/stockdb/{data,data1,mydb}/  可选 LevelDB 数据快照
 *   resources/quant/quant_terminal.py 策略层脚本
 * 返回清单；来源缺失记入 skipped，不抛错。
 */
export function stageSidecarAssets({ sources, resourcesRoot, logger = console } = {}) {
	const manifest = { stockdb: null, quant: null, skipped: [] };
	const stockdbRoot = path.join(resourcesRoot, 'stockdb');
	const quantRoot = path.join(resourcesRoot, 'quant');

	if (sources.stockdb?.bin && fs.existsSync(sources.stockdb.bin)) {
		fs.mkdirSync(stockdbRoot, { recursive: true });
		const target = path.join(stockdbRoot, sources.stockdb.binName);
		fs.copyFileSync(sources.stockdb.bin, target);
		let bytes = 0;
		try { bytes = fs.statSync(target).size; } catch { /* ignore */ }
		manifest.stockdb = { bin: target, binBytes: bytes, data: null, dataBytes: 0 };
		logger.log(`[sidecar] stockdb engine staged: ${target} (${(bytes / 1024 / 1024).toFixed(1)} MB)`);
	} else {
		manifest.skipped.push({ asset: 'stockdb.bin', reason: `source missing: ${sources.stockdb?.bin || '(unset)'}` });
		logger.warn(`[sidecar] stockdb engine not staged (set A_STOCK_STOCKDB_SRC); packaged app will run without the local market-data engine`);
	}

	if (manifest.stockdb && sources.stockdb?.data && fs.existsSync(sources.stockdb.data)) {
		let dataBytes = 0;
		for (const entry of ['data', 'data1', 'mydb']) {
			const source = path.join(sources.stockdb.data, entry);
			if (!fs.existsSync(source)) continue;
			dataBytes += copyTree(source, path.join(stockdbRoot, entry));
		}
		manifest.stockdb.data = stockdbRoot;
		manifest.stockdb.dataBytes = dataBytes;
		logger.log(`[sidecar] stockdb data snapshot staged: ${stockdbRoot} (${(dataBytes / 1024 / 1024 / 1024).toFixed(2)} GB)`);
	} else if (manifest.stockdb) {
		manifest.skipped.push({ asset: 'stockdb.data', reason: 'no data snapshot (A_STOCK_STOCKDB_DATA_SRC unset); first run starts an empty engine' });
		logger.warn('[sidecar] stockdb data snapshot not staged; packaged engine starts empty until data is provisioned');
	}

	if (sources.quant?.script && fs.existsSync(sources.quant.script)) {
		fs.mkdirSync(quantRoot, { recursive: true });
		const target = path.join(quantRoot, 'quant_terminal.py');
		fs.copyFileSync(sources.quant.script, target);
		let bytes = 0;
		try { bytes = fs.statSync(target).size; } catch { /* ignore */ }
		manifest.quant = { script: target, scriptBytes: bytes };
		logger.log(`[sidecar] quant terminal staged: ${target} (${(bytes / 1024).toFixed(0)} KB)`);
	} else {
		manifest.skipped.push({ asset: 'quant.script', reason: `source missing: ${sources.quant?.script || '(unset)'}` });
		logger.warn('[sidecar] quant terminal not staged (set A_STOCK_QUANT_SRC); packaged app will run without the strategy layer');
	}

	return manifest;
}
