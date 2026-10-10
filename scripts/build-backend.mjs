#!/usr/bin/env node
// 构建后端二进制并向其中注入 git 提交号（-ldflags "-X main.buildCommit=<sha>"），
// 使运行日志的 version 字段能精确标识实际运行的二进制来源。
//
// 采用 Node 实现而非直接写进 npm script，是为了规避 Windows cmd 下 $(...) 不展开的问题
// （npm 在 Windows 默认以 cmd.exe 执行 script 命令）。
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const backendRoot = path.join(repoRoot, 'backend');
const binDir = path.join(repoRoot, 'desktop', 'bin');
// 与 desktop/main.cjs 开发期路径解析保持一致（该处拼接 'bin'/'easy-stock-backend'，不带 .exe）。
const outputName = 'easy-stock-backend';

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
fs.mkdirSync(binDir, { recursive: true });
const output = path.join(binDir, outputName);

const args = ['build', '-trimpath'];
if (buildCommit) {
	args.push('-ldflags', `-X main.buildCommit=${buildCommit}`);
}
args.push('-o', output, './cmd/server');

const result = spawnSync('go', args, {
	cwd: backendRoot,
	env: { ...process.env, CGO_ENABLED: process.env.CGO_ENABLED || '0' },
	stdio: 'inherit',
});
if (result.error) {
	throw result.error;
}
if (result.status !== 0) {
	process.exit(result.status ?? 1);
}
console.log(`Backend built: ${path.relative(repoRoot, output)} (buildCommit=${buildCommit || '(unset)'})`);
