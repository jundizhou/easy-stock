const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const test = require('node:test');

// Use real Python imports and distribution metadata, without downloading Hermes
// or a model. The launcher mirrors the relocatable macOS runtime layout.
function fixture(t, version = '0.21.3') {
	if (process.platform === 'win32') { t.skip('fixture launcher uses POSIX shell'); return; }
	const probe = spawnSync('python3', ['-c', 'import sys; print(sys.executable)'], { encoding: 'utf8' });
	if (probe.status !== 0) { t.skip('Python 3 is required for the runtime fixture'); return; }
	const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock hermes-check-'));
	t.after(() => fs.rmSync(root, { recursive: true, force: true }));
	const runtimeRoot = path.join(root, 'runtime');
	const lib = path.join(runtimeRoot, 'lib');
	for (const module of ['hermes_cli', 'tui_gateway']) {
		fs.mkdirSync(path.join(lib, module), { recursive: true });
		fs.writeFileSync(path.join(lib, module, '__init__.py'), '');
	}
	const metadataRoot = path.join(lib, `hermes_agent-${version}.dist-info`);
	fs.mkdirSync(metadataRoot);
	fs.writeFileSync(path.join(metadataRoot, 'METADATA'), `Metadata-Version: 2.1\nName: hermes-agent\nVersion: ${version}\n`);
	const bin = path.join(runtimeRoot, 'venv', 'bin');
	fs.mkdirSync(bin, { recursive: true });
	const python = path.join(bin, 'python');
	const executable = `'${probe.stdout.trim().replaceAll("'", "'\\''")}'`;
	const launcher = `#!/bin/sh\nSCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)\nexport PYTHONPATH="$SCRIPT_DIR/../../lib"\nexec ${executable} "$@"\n`;
	fs.writeFileSync(python, launcher, { mode: 0o755 });
	const manifest = { version: '0.21.3', target_platform: process.platform, target_arch: process.arch };
	const manifestPath = path.join(runtimeRoot, 'runtime-manifest.json');
	fs.writeFileSync(manifestPath, JSON.stringify(manifest));
	return { root, runtimeRoot, lib, python, launcher, manifest, manifestPath };
}

test('verifies the installed Hermes distribution using the bundled launcher', async (t) => {
	const data = fixture(t);
	if (!data) return;
	const { verifyHermesRuntime } = await import('../scripts/verify-hermes-runtime.mjs');
	assert.deepEqual(verifyHermesRuntime(path.relative(process.cwd(), data.runtimeRoot)), { version: '0.21.3' });
});

test('rejects old installed Hermes even when the manifest claims the new version', async (t) => {
	const data = fixture(t, '0.19.0');
	if (!data) return;
	const { verifyHermesRuntime } = await import('../scripts/verify-hermes-runtime.mjs');
	assert.throws(() => verifyHermesRuntime(data.runtimeRoot), /installed version 0\.19\.0; expected 0\.21\.3/);
});

test('rejects stale manifests and packages for the wrong architecture', async (t) => {
	const data = fixture(t);
	if (!data) return;
	const { verifyHermesRuntime } = await import('../scripts/verify-hermes-runtime.mjs');
	fs.writeFileSync(data.manifestPath, JSON.stringify({ ...data.manifest, version: '0.19.0' }));
	assert.throws(() => verifyHermesRuntime(data.runtimeRoot), /manifest reports 0\.19\.0/);
	fs.writeFileSync(data.manifestPath, JSON.stringify({ ...data.manifest, target_arch: 'wrong-arch' }));
	assert.throws(() => verifyHermesRuntime(data.runtimeRoot), /does not match verifier/);
});

test('rejects Hermes modules imported from outside the shipped runtime', async (t) => {
	const data = fixture(t);
	if (!data) return;
	const { verifyHermesRuntime } = await import('../scripts/verify-hermes-runtime.mjs');
	const externalRoot = path.join(data.root, 'external');
	fs.mkdirSync(externalRoot);
	fs.renameSync(path.join(data.lib, 'hermes_cli'), path.join(externalRoot, 'hermes_cli'));
	fs.writeFileSync(data.python, data.launcher.replace('export PYTHONPATH="', 'export PYTHONPATH="$SCRIPT_DIR/../../../external:'), { mode: 0o755 });
	assert.throws(() => verifyHermesRuntime(data.runtimeRoot), /outside the bundled runtime/);
});

test('runtime preparation refuses to package a copied old Hermes runtime', async (t) => {
	const data = fixture(t, '0.19.0');
	if (!data) return;
	const { prepareHermesRuntime } = await import('../scripts/hermes-runtime.mjs');
	assert.throws(() => prepareHermesRuntime({ runtimeRoot: path.join(data.root, 'output'), sourcePath: data.runtimeRoot }), /installed 0\.19\.0; expected 0\.21\.3/);
});
