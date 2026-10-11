#!/usr/bin/env node
/**
 * CLI：校验本机是否只有「唯一版本」的 easy-stock。
 *
 *   node scripts/verify-deploy-consistency.mjs            # 全量（含已安装 App）
 *   node scripts/verify-deploy-consistency.mjs --ci       # CI：跳过已安装 App
 *
 * 退出码 0 = 唯一版本；1 = 存在差异（按 SOP 重新同步 / 装机）。
 */
import { collectChecks, installedDir, unpackedDir } from './deploy-consistency.mjs';

const skipInstalled = process.argv.includes('--ci') || process.env.A_STOCK_DEPLOY_CI === '1';

const { version, rows, installedPresent, usedInstalled } = await collectChecks({ skipInstalled });

console.log(`easy-stock 唯一版本校验  桌面版本 v${version}`);
console.log(`打包产物 ${unpackedDir()}`);
console.log(`已安装   ${installedDir() || '(本机未安装)'}  ${installedPresent ? '' : '（缺失，跳过相关项）'}`);
console.log('-'.repeat(78));
for (const row of rows) {
	console.log(`${row.ok ? '✓' : '✗'} ${row.name.padEnd(34)} ${row.detail}`);
}
const failed = rows.filter((row) => !row.ok);
console.log('-'.repeat(78));
if (failed.length === 0) {
	console.log(`全部 ${rows.length} 项一致 → 唯一版本 ✓${usedInstalled ? '' : '（未校验装机副本）'}`);
	process.exit(0);
}
console.log(`${failed.length}/${rows.length} 项不一致 → 禁止发布，先跑 npm run sync:artifacts 再打包 / npm run install:local 装机`);
process.exit(1);
