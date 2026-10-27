import fs from 'node:fs';
import { spawnSync } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { HERMES_AGENT_VERSION, hermesRuntimePython, prepareHermesRuntime } from './hermes-runtime.mjs';
import { verifyHermesRuntime } from './verify-hermes-runtime.mjs';
import { prepareWechatDependencies } from './wechat-runtime.mjs';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const runtimeRoot = path.join(desktopRoot, 'resources', 'hermes-runtime');

const installedVersion = readInstalledVersion(runtimeRoot);
let valid = false;
if (installedVersion === HERMES_AGENT_VERSION && hasRuntimePython(runtimeRoot)) {
	try {
		verifyHermesRuntime(runtimeRoot);
		valid = true;
	} catch (error) {
		console.warn(`Hermes runtime needs repair: ${error.message}`);
	}
}
if (valid) {
	console.log(`Hermes runtime ready: ${installedVersion}`);
} else {
	console.log(`Preparing Hermes runtime ${HERMES_AGENT_VERSION}${installedVersion ? ` (found ${installedVersion})` : ''}…`);
	const manifest = prepareHermesRuntime({ runtimeRoot });
	if (manifest.version !== HERMES_AGENT_VERSION) {
		throw new Error(`Hermes runtime prepared as ${manifest.version}; expected ${HERMES_AGENT_VERSION}`);
	}
	console.log(`Hermes runtime ready: ${manifest.version}`);
}

function readInstalledVersion(root) {
	try {
		const manifest = JSON.parse(fs.readFileSync(path.join(root, 'runtime-manifest.json'), 'utf8'));
		return typeof manifest.version === 'string' ? manifest.version.trim() : '';
	} catch {
		return '';
	}
}

function hasRuntimePython(root) {
	try {
		return fs.statSync(hermesRuntimePython(root)).isFile();
	} catch {
		return false;
	}
}

// The development desktop uses the same Python for the bundled WeChat service.
// Rebuilding Hermes replaces its site-packages, so restore those dependencies.
const wechatSource = path.join(desktopRoot, 'resources', 'wechat-download-api');
if (fs.existsSync(path.join(wechatSource, 'app.py'))) {
	const python = hermesRuntimePython(runtimeRoot);
	const check = spawnSync(python, ['-c', 'import bs4, curl_cffi, markdownify, xhtml2pdf, docx, openpyxl, ebooklib'], { encoding: 'utf8', env: { ...process.env, PYTHONNOUSERSITE: '1' } });
	if (check.status !== 0) {
		console.log('Preparing WeChat service dependencies…');
		prepareWechatDependencies({ runtimeRoot, sourceRoot: wechatSource });
	}
}
