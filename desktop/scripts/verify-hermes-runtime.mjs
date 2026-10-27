import fs from 'node:fs';
import path from 'node:path';
import { HERMES_AGENT_VERSION, HERMES_SOURCE_SHA256, verifyHermesPython } from './hermes-runtime.mjs';

// Verify source identity and actual imports using the interpreter shipped to users.
export function verifyHermesRuntime(runtimeRoot, expectedFiles) {
	runtimeRoot = path.resolve(runtimeRoot);
	const manifest = JSON.parse(fs.readFileSync(path.join(runtimeRoot, 'runtime-manifest.json'), 'utf8'));
	if (manifest.version !== HERMES_AGENT_VERSION || manifest.source_sha256 !== HERMES_SOURCE_SHA256) {
		throw new Error(`Bundled Hermes manifest reports ${manifest.version}; expected ${HERMES_AGENT_VERSION} and the pinned source`);
	}
	if (manifest.target_platform !== process.platform || manifest.target_arch !== process.arch) {
		throw new Error(`Bundled Hermes target ${manifest.target_platform}/${manifest.target_arch} does not match verifier ${process.platform}/${process.arch}`);
	}
	const installed = verifyHermesPython(runtimeRoot, expectedFiles);
	console.log(`Bundled Hermes verified: ${installed.version} (${manifest.target_platform}/${manifest.target_arch})`);
	return installed;
}
