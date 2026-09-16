import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { HERMES_AGENT_VERSION, hermesRuntimePython } from './hermes-runtime.mjs';

// Check the installed distribution and import locations using the interpreter
// shipped to users. A manifest alone cannot prove what is inside the package.
export function verifyHermesRuntime(runtimeRoot) {
	runtimeRoot = path.resolve(runtimeRoot);
	const manifest = JSON.parse(fs.readFileSync(path.join(runtimeRoot, 'runtime-manifest.json'), 'utf8'));
	if (manifest.version !== HERMES_AGENT_VERSION) {
		throw new Error(`Bundled Hermes manifest reports ${manifest.version}; expected ${HERMES_AGENT_VERSION}`);
	}
	if (manifest.target_platform !== process.platform || manifest.target_arch !== process.arch) {
		throw new Error(`Bundled Hermes target ${manifest.target_platform}/${manifest.target_arch} does not match verifier ${process.platform}/${process.arch}`);
	}
	const script = `
import importlib.metadata as metadata
import json
from pathlib import Path
import sys
import hermes_cli
import tui_gateway

root = Path(sys.argv[1]).resolve()
expected = sys.argv[2]
distribution = metadata.distribution("hermes-agent")
if distribution.version != expected:
    raise RuntimeError(f"Bundled Hermes installed version {distribution.version}; expected {expected}")
locations = [Path(distribution.locate_file("")).resolve()]
locations.extend(Path(module.__file__).resolve() for module in (hermes_cli, tui_gateway))
for location in locations:
    if not location.is_relative_to(root):
        raise RuntimeError(f"Hermes resolved outside the bundled runtime: {location}")
print(json.dumps({"version": distribution.version}))
`;
	const env = { ...process.env, PYTHONNOUSERSITE: '1', PYTHONDONTWRITEBYTECODE: '1' };
	delete env.PYTHONPATH;
	delete env.PYTHONHOME;
	delete env.VIRTUAL_ENV;
	const python = hermesRuntimePython(runtimeRoot);
	const args = [...(process.platform === 'win32' ? ['-I'] : []), '-c', script, path.resolve(runtimeRoot), HERMES_AGENT_VERSION];
	const result = spawnSync(python, args, { cwd: runtimeRoot, encoding: 'utf8', env, timeout: 60_000 });
	if (result.error) throw result.error;
	if (result.status !== 0) {
		throw new Error(`Bundled Hermes verification failed: ${(result.stderr || result.stdout || '').trim() || `exit status ${result.status}`}`);
	}
	const installed = JSON.parse(result.stdout.trim());
	if (installed.version !== HERMES_AGENT_VERSION) throw new Error('Bundled Hermes did not confirm the expected version');
	console.log(`Bundled Hermes verified: ${installed.version} (${manifest.target_platform}/${manifest.target_arch})`);
	return installed;
}
