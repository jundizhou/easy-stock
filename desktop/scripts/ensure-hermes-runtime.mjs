import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { HERMES_AGENT_VERSION, hermesRuntimePython, prepareHermesRuntime } from './hermes-runtime.mjs';

const desktopRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const runtimeRoot = path.join(desktopRoot, 'resources', 'hermes-runtime');

const installedVersion = readInstalledVersion(runtimeRoot);
if (installedVersion === HERMES_AGENT_VERSION && hasRuntimePython(runtimeRoot)) {
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
