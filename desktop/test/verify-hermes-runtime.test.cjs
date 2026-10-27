const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const test = require('node:test');

// Exercise actual imports and version checks with a small, isolated source tree.
async function fixture(t) {
	if (process.platform === 'win32') { t.skip('fixture launcher uses POSIX shell'); return; }
	const probe = spawnSync('python3', ['-c', 'import sys; print(sys.executable); print("%s.%s" % sys.version_info[:2])'], { encoding: 'utf8' });
	if (probe.status !== 0 || !probe.stdout.includes('3.14')) { t.skip('Python 3.14 is required for the runtime fixture'); return; }
	const { HERMES_AGENT_VERSION, HERMES_SOURCE_SHA256 } = await import('../scripts/hermes-runtime.mjs');
	const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock hermes-check-'));
	t.after(() => fs.rmSync(root, { recursive: true, force: true }));
	const runtimeRoot = path.join(root, 'runtime');
	const source = path.join(runtimeRoot, 'source');
	const files = {
		'hermes_cli/__init__.py': `__version__ = '${HERMES_AGENT_VERSION}'\n`,
		'tui_gateway/__init__.py': '',
		'tui_gateway/entry.py': '',
	};
	const hashes = {};
	for (const [name, contents] of Object.entries(files)) {
		const file = path.join(source, name);
		fs.mkdirSync(path.dirname(file), { recursive: true });
		fs.writeFileSync(file, contents);
		hashes[name] = crypto.createHash('sha256').update(contents).digest('hex');
	}
	const stamp = { baseVersion: HERMES_AGENT_VERSION };
	fs.writeFileSync(path.join(source, 'install-stamp.json'), JSON.stringify(stamp));
	const bin = path.join(runtimeRoot, 'venv', 'bin');
	fs.mkdirSync(bin, { recursive: true });
	const python = path.join(bin, 'python');
	const executable = `'${probe.stdout.split('\n')[0].replaceAll("'", "'\\''")}'`;
	const launcher = `#!/bin/sh\nSCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)\nexport PYTHONPATH="$SCRIPT_DIR/../../source"\nexec ${executable} "$@"\n`;
	fs.writeFileSync(python, launcher, { mode: 0o755 });
	const manifest = { version: HERMES_AGENT_VERSION, source_sha256: HERMES_SOURCE_SHA256, target_platform: process.platform, target_arch: process.arch };
	const manifestPath = path.join(runtimeRoot, 'runtime-manifest.json');
	fs.writeFileSync(manifestPath, JSON.stringify(manifest));
	return { root, runtimeRoot, source, hashes, stamp, manifest, manifestPath, python, launcher };
}

test('verifies source identity and imports with the bundled launcher', async (t) => {
	const data = await fixture(t); if (!data) return;
	const { verifyHermesRuntime } = await import('../scripts/verify-hermes-runtime.mjs');
	assert.deepEqual(verifyHermesRuntime(data.runtimeRoot, data.hashes), { version: data.manifest.version });
});

test('rejects modified source and a stale source version', async (t) => {
	const data = await fixture(t); if (!data) return;
	const { verifyHermesRuntime } = await import('../scripts/verify-hermes-runtime.mjs');
	fs.writeFileSync(path.join(data.source, 'tui_gateway/entry.py'), '# changed\n');
	assert.throws(() => verifyHermesRuntime(data.runtimeRoot, data.hashes), /source verification failed/);
	fs.writeFileSync(path.join(data.source, 'tui_gateway/entry.py'), '');
	fs.writeFileSync(path.join(data.source, 'install-stamp.json'), JSON.stringify({ ...data.stamp, baseVersion: '0.19.0' }));
	assert.throws(() => verifyHermesRuntime(data.runtimeRoot, data.hashes), /source version 0\.19\.0/);
});

test('rejects stale manifests and packages for the wrong architecture', async (t) => {
	const data = await fixture(t); if (!data) return;
	const { verifyHermesRuntime } = await import('../scripts/verify-hermes-runtime.mjs');
	fs.writeFileSync(data.manifestPath, JSON.stringify({ ...data.manifest, version: '0.19.0' }));
	assert.throws(() => verifyHermesRuntime(data.runtimeRoot, data.hashes), /manifest reports 0\.19\.0/);
	fs.writeFileSync(data.manifestPath, JSON.stringify({ ...data.manifest, target_arch: 'wrong-arch' }));
	assert.throws(() => verifyHermesRuntime(data.runtimeRoot, data.hashes), /does not match verifier/);
});

test('runtime preparation refuses an old copied runtime without deleting the existing one', async (t) => {
	const data = await fixture(t); if (!data) return;
	const { prepareHermesRuntime } = await import('../scripts/hermes-runtime.mjs');
	const output = path.join(data.root, 'output');
	fs.mkdirSync(output);
	fs.writeFileSync(path.join(output, 'keep'), 'existing');
	assert.throws(() => prepareHermesRuntime({ runtimeRoot: output, sourcePath: data.runtimeRoot }), /source verification failed/);
	assert.equal(fs.readFileSync(path.join(output, 'keep'), 'utf8'), 'existing');
});
