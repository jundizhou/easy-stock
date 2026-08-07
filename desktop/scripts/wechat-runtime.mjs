import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

import { hermesRuntimePython } from './hermes-runtime.mjs';

export const WECHAT_DOWNLOAD_API_REVISION = process.env.WECHAT_DOWNLOAD_API_REVISION || '043c2f9828401220a00b7b125686b334581745e0';
export const WECHAT_DOWNLOAD_API_REPOSITORY = 'https://github.com/tmwgsicp/wechat-download-api';

const pythonDependencies = [
	'curl_cffi>=0.7.0',
	'beautifulsoup4>=4.12.0',
	'markdownify>=0.11.0',
	'xhtml2pdf>=0.2.16',
	'python-docx>=1.1.0',
	'openpyxl>=3.1.0',
	'EbookLib>=0.18',
];

export function prepareWechatRuntime({ resourcesRoot, runtimeRoot, sourcePath = process.env.WECHAT_DOWNLOAD_API_SOURCE || '', uv = process.env.UV || 'uv' } = {}) {
	if (!resourcesRoot) throw new Error('resourcesRoot is required');
	if (!runtimeRoot) throw new Error('runtimeRoot is required');
	const resolvedResourcesRoot = path.resolve(resourcesRoot);
	const resolvedRuntimeRoot = path.resolve(runtimeRoot);
	const targetRoot = path.join(resolvedResourcesRoot, 'wechat-download-api');
	fs.rmSync(targetRoot, { recursive: true, force: true });

	const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'a-stock-wechat-source-'));
	try {
		const resolvedSource = sourcePath ? path.resolve(sourcePath) : downloadSource(tempRoot);
		if (!fs.existsSync(path.join(resolvedSource, 'app.py'))) throw new Error(`wechat-download-api source is incomplete: ${resolvedSource}`);
		fs.cpSync(resolvedSource, targetRoot, { recursive: true });
	} finally {
		fs.rmSync(tempRoot, { recursive: true, force: true });
	}

	const manifest = {
		schema_version: 1,
		package: 'wechat-download-api',
		repository: WECHAT_DOWNLOAD_API_REPOSITORY,
		revision: WECHAT_DOWNLOAD_API_REVISION,
		license: 'AGPL-3.0-only',
		created_at: new Date().toISOString(),
	};
	fs.writeFileSync(path.join(targetRoot, 'easy-stock-wechat-runtime.json'), `${JSON.stringify(manifest, null, 2)}\n`);
	fs.writeFileSync(path.join(targetRoot, 'EASY_STOCK_SOURCE_NOTICE.md'), sourceNotice());

	const python = hermesRuntimePython(resolvedRuntimeRoot);
	if (process.platform === 'win32') {
		run(uv, ['pip', 'install', '--python', python, '--link-mode', 'copy', ...pythonDependencies], resolvedRuntimeRoot);
	} else {
		const sitePackages = runtimeSitePackages(resolvedRuntimeRoot);
		const basePython = bundledRuntimePython(resolvedRuntimeRoot);
		run(uv, ['pip', 'install', '--target', sitePackages, '--python', basePython, '--link-mode', 'copy', ...pythonDependencies], resolvedRuntimeRoot);
	}
	run(python, ['-c', 'import app, bs4, curl_cffi, markdownify, xhtml2pdf, docx, openpyxl, ebooklib'], targetRoot, {
		PYTHONNOUSERSITE: '1',
		ENABLE_MCP: '0',
		SKIP_BACKGROUND_TASKS: '1',
	});
	return manifest;
}

function runtimeSitePackages(runtimeRoot) {
	const libRoot = path.join(runtimeRoot, 'venv', 'lib');
	const pythonLib = fs.readdirSync(libRoot, { withFileTypes: true }).find((entry) => entry.isDirectory() && entry.name.startsWith('python'));
	if (!pythonLib) throw new Error(`Python site-packages directory not found in ${libRoot}`);
	return path.join(libRoot, pythonLib.name, 'site-packages');
}

function bundledRuntimePython(runtimeRoot) {
	const binRoot = path.join(runtimeRoot, 'python', 'bin');
	const candidate = fs.readdirSync(binRoot).find((name) => /^python3\.\d+$/.test(name));
	if (!candidate) throw new Error(`Bundled Python executable not found in ${binRoot}`);
	return path.join(binRoot, candidate);
}

function downloadSource(tempRoot) {
	const archiveURL = `${WECHAT_DOWNLOAD_API_REPOSITORY}/archive/${WECHAT_DOWNLOAD_API_REVISION}.tar.gz`;
	const archivePath = path.join(tempRoot, 'source.tar.gz');
	run('curl', ['-fsSL', '--retry', '4', '--retry-delay', '2', '--retry-all-errors', '--connect-timeout', '20', archiveURL, '-o', archivePath], tempRoot);
	run('tar', ['-xzf', archivePath, '-C', tempRoot], tempRoot);
	const extracted = fs.readdirSync(tempRoot, { withFileTypes: true })
		.find((entry) => entry.isDirectory() && entry.name.startsWith('wechat-download-api-'));
	if (!extracted) throw new Error('Unable to locate extracted wechat-download-api source');
	return path.join(tempRoot, extracted.name);
}

function sourceNotice() {
	return `# Bundled third-party service\n\n` +
		`This directory contains the unmodified source code of [tmwgsicp/wechat-download-api](${WECHAT_DOWNLOAD_API_REPOSITORY}) at commit \`${WECHAT_DOWNLOAD_API_REVISION}\`.\n\n` +
		`It is distributed under the GNU Affero General Public License v3.0 only. The complete license is included in \`LICENSE\`. ` +
		`easy-stock starts this service only on the local loopback interface and stores its runtime data in the application's user-data directory.\n`;
}

function run(command, args, cwd, env = {}) {
	const result = spawnSync(command, args, { cwd, stdio: 'inherit', env: { ...process.env, ...env } });
	if (result.error) throw result.error;
	if (result.status !== 0) throw new Error(`${command} exited with status ${result.status ?? 1}`);
}
