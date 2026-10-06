const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

async function loadModule() {
	return import('../scripts/sidecar-assets.mjs');
}

function tempDir() {
	return fs.mkdtempSync(path.join(os.tmpdir(), 'sidecar-assets-'));
}

test('resolveSidecarSources 环境变量优先于默认值', async () => {
	const { resolveSidecarSources } = await loadModule();
	const sources = resolveSidecarSources({
		env: { A_STOCK_STOCKDB_SRC: 'D:/engines/stockdb.exe', A_STOCK_QUANT_SRC: 'D:/quant/quant_terminal.py' },
		platform: 'win32',
		defaults: { stockdbBin: 'C:/default/stockdb.exe', quantScript: 'C:/default/quant_terminal.py' },
	});
	assert.equal(sources.stockdb.bin, 'D:/engines/stockdb.exe');
	assert.equal(sources.stockdb.binName, 'stockdb.exe');
	assert.equal(sources.quant.script, 'D:/quant/quant_terminal.py');
});

test('resolveSidecarSources 无环境变量时回退默认值，win32/posix 二进制名不同', async () => {
	const { resolveSidecarSources } = await loadModule();
	const win = resolveSidecarSources({ env: {}, platform: 'win32', defaults: { stockdbBin: 'C:/default/stockdb.exe' } });
	assert.equal(win.stockdb.bin, 'C:/default/stockdb.exe');
	assert.equal(win.stockdb.binName, 'stockdb.exe');
	const posix = resolveSidecarSources({ env: {}, platform: 'linux', defaults: {} });
	assert.equal(posix.stockdb.binName, 'stockdb');
});

test('stageSidecarAssets 复制引擎与脚本并产出清单', async () => {
	const { stageSidecarAssets } = await loadModule();
	const work = tempDir();
	const engineSrc = path.join(work, 'engine', 'stockdb.exe');
	const quantSrc = path.join(work, 'quant_terminal.py');
	const dataSrc = path.join(work, 'snapshot', 'data');
	fs.mkdirSync(path.dirname(engineSrc), { recursive: true });
	fs.mkdirSync(path.dirname(quantSrc), { recursive: true });
	fs.mkdirSync(dataSrc, { recursive: true });
	fs.writeFileSync(engineSrc, 'MZ-engine');
	fs.writeFileSync(quantSrc, 'print("terminal")');
	fs.writeFileSync(path.join(dataSrc, '000001.ldb'), 'x'.repeat(2048));

	const resourcesRoot = path.join(work, 'resources');
	const manifest = stageSidecarAssets({
		sources: {
			stockdb: { binName: 'stockdb.exe', bin: engineSrc, data: path.join(work, 'snapshot') },
			quant: { script: quantSrc },
		},
		resourcesRoot,
		logger: { log: () => {}, warn: () => {} },
	});

	assert.ok(manifest.stockdb, 'stockdb manifest entry');
	assert.equal(manifest.stockdb.bin, path.join(resourcesRoot, 'stockdb', 'stockdb.exe'));
	assert.ok(fs.existsSync(manifest.stockdb.bin));
	assert.ok(manifest.stockdb.dataBytes >= 2048, 'data snapshot copied');
	assert.equal(manifest.quant.script, path.join(resourcesRoot, 'quant', 'quant_terminal.py'));
	assert.ok(fs.existsSync(manifest.quant.script));
	assert.equal(manifest.skipped.length, 0);
});

test('stageSidecarAssets 来源缺失时跳过且不抛错', async () => {
	const { stageSidecarAssets } = await loadModule();
	const work = tempDir();
	const manifest = stageSidecarAssets({
		sources: {
			stockdb: { binName: 'stockdb.exe', bin: path.join(work, 'missing.exe'), data: '' },
			quant: { script: path.join(work, 'missing.py') },
		},
		resourcesRoot: path.join(work, 'resources'),
		logger: { log: () => {}, warn: () => {} },
	});
	assert.equal(manifest.stockdb, null);
	assert.equal(manifest.quant, null);
	assert.deepEqual(manifest.skipped.map((entry) => entry.asset), ['stockdb.bin', 'quant.script']);
});
