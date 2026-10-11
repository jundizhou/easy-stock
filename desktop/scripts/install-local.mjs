#!/usr/bin/env node
/**
 * 装机：用 win-unpacked 全量覆盖本机已安装的 easy-stock。
 *
 * 规范：只允许「整目录覆盖」，禁止只换 exe（会造成 exe 与 resources 版本错配）。
 * 覆盖前自动把旧 exe / 旧 app.asar 移到 C:\quant\backup\easy-stock-installed-<时间戳>\，覆盖后自动跑一致性校验。
 *
 *   node scripts/install-local.mjs          # 正常装机
 *   node scripts/install-local.mjs --dry    # 只演练，不落盘
 */
import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

import { collectChecks, installedDir, unpackedDir } from './deploy-consistency.mjs';

const dryRun = process.argv.includes('--dry');
const src = unpackedDir();
const dst = installedDir();
const RESOURCE_DIRS = [
	'agent-browser',
	'backend',
	'codex-runtime',
	'frontend',
	'hermes-runtime',
	'quant',
	'stockdb',
	'wechat-download-api',
];

function fail(message) {
	console.error(`✗ ${message}`);
	process.exit(1);
}

if (!dst) fail('无法确定安装目录（LOCALAPPDATA 为空），可设 EASY_STOCK_INSTALLED_DIR 指定');
if (!fs.existsSync(path.join(src, 'easy-stock.exe'))) fail(`打包产物缺失：${src}（先打包）`);

// 应用运行中则拒绝覆盖（exe 会被占用）。
// tasklist 在部分受限环境（沙箱/EBUSY）下不可用，因此必须再做文件锁探测：
// 运行中的 exe / asar 无法以写模式打开（EBUSY），这是最终裁决。
function fileLocked(file) {
	try {
		const fd = fs.openSync(file, 'r+');
		fs.closeSync(fd);
		return false;
	} catch (error) {
		return error.code === 'EBUSY' || error.code === 'EPERM';
	}
}

if (process.platform === 'win32') {
	const probe = spawnSync('tasklist', ['/FI', 'IMAGENAME eq easy-stock.exe'], { encoding: 'utf8' });
	if (!(probe.error || probe.status !== 0) && (probe.stdout || '').includes('easy-stock.exe')) {
		fail('easy-stock 正在运行，请先退出应用再装机');
	}
	for (const target of [path.join(dst, 'easy-stock.exe'), path.join(dst, 'resources', 'app.asar')]) {
		if (fs.existsSync(target) && fileLocked(target)) {
			fail(`目标文件被占用（应用可能正在运行），请先退出 easy-stock 再装机：${target}`);
		}
	}
}

const stamp = new Date().toISOString().replace(/[-:]/g, '').replace(/\..+/, '').replace('T', '_');
const backupRoot = path.join('C:\\quant\\backup', `easy-stock-installed-${stamp}`);
console.log(`装机：${src}\n  → ${dst}\n备份：${backupRoot}${dryRun ? '\n（演练模式，不落盘）' : ''}`);

if (!dryRun) {
	fs.mkdirSync(backupRoot, { recursive: true });
	const backupFile = (file, name) => {
		if (!fs.existsSync(file)) return;
		fs.copyFileSync(file, path.join(backupRoot, name));
		fs.rmSync(file, { force: true });
		console.log(`· 已备份 ${name}`);
	};
	backupFile(path.join(dst, 'easy-stock.exe'), 'easy-stock.exe.bak');
	backupFile(path.join(dst, 'resources', 'app.asar'), 'app.asar.bak');

	fs.mkdirSync(path.join(dst, 'resources', 'resources'), { recursive: true });
	fs.copyFileSync(path.join(src, 'easy-stock.exe'), path.join(dst, 'easy-stock.exe'));
	fs.copyFileSync(path.join(src, 'resources', 'app.asar'), path.join(dst, 'resources', 'app.asar'));
	for (const dir of RESOURCE_DIRS) {
		const from = path.join(src, 'resources', 'resources', dir);
		const to = path.join(dst, 'resources', 'resources', dir);
		if (!fs.existsSync(from)) continue;
		fs.rmSync(to, { recursive: true, force: true });
		fs.cpSync(from, to, { recursive: true });
		console.log(`· 覆盖 ${dir}`);
	}
	console.log('✓ 覆盖完成');
}

const { version, rows } = await collectChecks({});
console.log('-'.repeat(78));
for (const row of rows) console.log(`${row.ok ? '✓' : '✗'} ${row.name.padEnd(34)} ${row.detail}`);
const failed = rows.filter((row) => !row.ok);
console.log('-'.repeat(78));
if (failed.length) fail(`${failed.length}/${rows.length} 项不一致，请检查装机流程`);
console.log(`全部 ${rows.length} 项一致 → 唯一版本 v${version} ✓`);
