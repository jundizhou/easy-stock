const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const desktopRoot = path.resolve(__dirname, '..');

test('唯一版本：构建产物 / 打包资源 / 打包产物 / 已安装 App 四处一致', async () => {
	const mod = await import('../scripts/deploy-consistency.mjs');
	const { rows, installedPresent } = await mod.collectChecks({ deep: false });
	assert.ok(rows.length > 0, '校验项不应为空');
	if (!installedPresent) {
		// CI / 未装机环境：至少保证前三个副本一致
		console.log('  （本机未检测到已安装 App，跳过装机副本相关项）');
	}
	const failed = rows.filter((row) => !row.ok);
	assert.deepStrictEqual(
		failed.map((row) => `${row.name} → ${row.detail}`),
		[],
		`存在不一致副本，请跑 npm run sync:artifacts 后重新打包，再 npm run install:local`,
	);
});

test('唯一版本：打包脚本必须暴露同步 / 校验 / 装机三个入口', () => {
	const pkg = JSON.parse(fs.readFileSync(path.join(desktopRoot, 'package.json'), 'utf8'));
	for (const script of ['sync:artifacts', 'verify:deploy', 'install:local', 'package:windows:offline']) {
		assert.ok(pkg.scripts && pkg.scripts[script], `package.json 缺少 npm run ${script}`);
	}
	for (const file of ['sync-build-artifacts.mjs', 'verify-deploy-consistency.mjs', 'install-local.mjs', 'deploy-consistency.mjs']) {
		assert.ok(fs.existsSync(path.join(desktopRoot, 'scripts', file)), `缺少 scripts/${file}`);
	}
});

test('唯一版本：打包资源目录不得残留多代前端 bundle', () => {
	const assetsDir = path.join(desktopRoot, 'resources', 'frontend', 'dist', 'assets');
	if (!fs.existsSync(assetsDir)) return;
	const bundles = fs.readdirSync(assetsDir).filter((name) => /^index-.*\.(js|css)$/.test(name));
	assert.ok(bundles.length <= 2, `前端 bundle 出现多代残留：${bundles.join(', ')}（同步时须先清空再拷）`);
});

test('唯一版本：装机脚本必须带运行中保护（tasklist + 文件锁双探测）', () => {
	const source = fs.readFileSync(path.join(desktopRoot, 'scripts', 'install-local.mjs'), 'utf8');
	assert.ok(source.includes("spawnSync('tasklist'"), '装机脚本缺少 tasklist 运行中检测');
	// tasklist 在沙箱/受限环境会 EBUSY 静默失效，文件锁探测是最终裁决，缺了就会覆盖运行中的 exe。
	assert.ok(source.includes("error.code === 'EBUSY'"), '装机脚本缺少文件锁（EBUSY）兜底探测');
	assert.ok(/fileLocked\(target\)/.test(source), '装机脚本未对目标文件做锁定探测');
});
