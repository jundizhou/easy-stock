#!/usr/bin/env node
/**
 * 把构建产物同步进打包资源目录 desktop/resources/。
 *
 * 规范：extraResources 恒从 desktop/resources 进包，因此「构建」与「打包」之间必须有且只有这一步。
 * 手工 cp 容易漏掉后端或留下多代前端 bundle，这里统一处理：
 *   ① 前端：先清空再整目录拷贝（杜绝多代 index-*.js/css 残留）
 *   ② 后端：desktop/bin/easy-stock-backend → desktop/resources/backend/easy-stock-backend.exe（旧件自动加 .bak-<时间戳>）
 */
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { fileURLToPath } from 'node:url';

import { resolveBackendFile } from './deploy-consistency.mjs';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const repoRoot = path.resolve(desktopRoot, '..');
const backendName = process.platform === 'win32' ? 'easy-stock-backend.exe' : 'easy-stock-backend';

const stamp = () => new Date().toISOString().replace(/[-:]/g, '').replace(/\..+/, '').replace('T', '_');
const sha = (file) => crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex').slice(0, 16);

function fail(message) {
	console.error(`✗ ${message}`);
	process.exit(1);
}

// ---- ① 前端 ----
const frontendSource = path.join(repoRoot, 'frontend', 'dist');
const frontendTarget = path.join(desktopRoot, 'resources', 'frontend', 'dist');
if (!fs.existsSync(path.join(frontendSource, 'index.html'))) {
	fail(`前端产物缺失：${frontendSource}（先跑 npm --workspace frontend run build）`);
}
fs.rmSync(frontendTarget, { recursive: true, force: true });
fs.mkdirSync(path.dirname(frontendTarget), { recursive: true });
fs.cpSync(frontendSource, frontendTarget, { recursive: true });
const assets = fs.readdirSync(path.join(frontendTarget, 'assets')).filter((n) => /^index-.*\.(js|css)$/.test(n));
if (assets.length !== 2) fail(`前端产物异常：index-*.js/css 应为 2 个，实际 ${assets.length} 个（${assets.join(', ')}）`);
console.log(`✓ 前端已同步（清空后重拷）：${assets.join(', ')}`);

// ---- ② 后端 ----
const backendSource = resolveBackendFile(path.join(desktopRoot, 'bin'));
if (!fs.existsSync(backendSource)) {
	fail(`后端产物缺失：${backendSource}（先跑 npm run build:backend）`);
}
let backendTarget = resolveBackendFile(path.join(desktopRoot, 'resources', 'backend'));
if (!fs.existsSync(backendTarget)) {
	// resources 里尚不存在时按平台规范命名
	backendTarget = path.join(desktopRoot, 'resources', 'backend', backendName);
}
fs.mkdirSync(path.dirname(backendTarget), { recursive: true });
const sourceSha = sha(backendSource);
if (fs.existsSync(backendTarget) && sha(backendTarget) === sourceSha) {
	console.log(`✓ 后端已是最新（sha ${sourceSha}），跳过覆盖`);
} else {
	if (fs.existsSync(backendTarget)) {
		const backup = `${backendTarget}.bak-${stamp()}`;
		fs.renameSync(backendTarget, backup);
		console.log(`· 旧后端已备份 → ${path.basename(backup)}`);
	}
	fs.copyFileSync(backendSource, backendTarget);
	console.log(`✓ 后端已同步：sha ${sha(backendTarget)}`);
}

console.log('\n下一步：');
console.log('  在线打包  npm --workspace desktop run release:windows');
console.log('  离线打包  NODE_OPTIONS="" PATH="/c/Windows/System32:$PATH" A_STOCK_ELECTRON_DIST=<repo>/node_modules/electron/dist \\');
console.log('            node desktop/scripts/electron-builder.mjs windows dir && npm --workspace desktop run verify:deploy');
