const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const test = require('node:test');

// Exercise actual imports, including the isolated standalone Windows layout.
async function fixture(t) {
	let python = process.env.EASY_STOCK_HERMES_TEST_PYTHON;
	if (!python) {
		const managed = spawnSync('uv', ['python', 'find', '3.14'], { encoding: 'utf8', windowsHide: true });
		python = managed.status === 0 ? managed.stdout.trim() : 'python3';
	}
	const probe = spawnSync(python, ['-I', '-c', 'import sys; print(sys.executable); print(sys.base_prefix); print("%s.%s" % sys.version_info[:2])'], { encoding: 'utf8', windowsHide: true });
	if (probe.status !== 0 || probe.stdout.trim().split(/\r?\n/).at(-1) !== '3.14') { t.skip('Python 3.14 is required for the runtime fixture'); return; }
	const { HERMES_AGENT_VERSION, HERMES_SOURCE_SHA256, robustRemove } = await import('../scripts/hermes-runtime.mjs');
	const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock Hermes 校验-'));
	t.after(() => {
		assert.equal(path.dirname(path.resolve(root)), path.resolve(os.tmpdir()));
		robustRemove(root);
	});
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
	let importPath;
	if (process.platform === 'win32') {
		fs.cpSync(probe.stdout.split(/\r?\n/)[1], path.join(runtimeRoot, 'python'), { recursive: true, dereference: true });
		const site = path.join(runtimeRoot, 'python', 'Lib', 'site-packages');
		fs.mkdirSync(site, { recursive: true });
		importPath = path.join(site, 'hermes-source.pth');
		fs.writeFileSync(importPath, '../../../source\n');
	} else {
		const bin = path.join(runtimeRoot, 'venv', 'bin');
		fs.mkdirSync(bin, { recursive: true });
		const python = path.join(bin, 'python');
		const executable = `'${probe.stdout.split(/\r?\n/)[0].replaceAll("'", "'\\''")}'`;
		const launcher = `#!/bin/sh\nSCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)\nexport PYTHONPATH="$SCRIPT_DIR/../../source"\nexec ${executable} "$@"\n`;
		fs.writeFileSync(python, launcher, { mode: 0o755 });
		importPath = python;
	}
	const manifest = { version: HERMES_AGENT_VERSION, source_sha256: HERMES_SOURCE_SHA256, target_platform: process.platform, target_arch: process.arch };
	const manifestPath = path.join(runtimeRoot, 'runtime-manifest.json');
	fs.writeFileSync(manifestPath, JSON.stringify(manifest));
	return { root, runtimeRoot, source, hashes, stamp, manifest, manifestPath, importPath };
}

test('verifies actual imports with spaces, Unicode paths and runtime relocation', async (t) => {
	const data = await fixture(t); if (!data) return;
	const { verifyHermesRuntime } = await import('../scripts/verify-hermes-runtime.mjs');
	assert.deepEqual(verifyHermesRuntime(path.relative(process.cwd(), data.runtimeRoot), data.hashes), { version: data.manifest.version });
	const moved = path.join(data.root, 'moved runtime 迁移');
	fs.renameSync(data.runtimeRoot, moved);
	assert.deepEqual(verifyHermesRuntime(moved, data.hashes), { version: data.manifest.version });
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
	fs.writeFileSync(data.manifestPath, JSON.stringify({ ...data.manifest, source_sha256: 'wrong-source' }));
	assert.throws(() => verifyHermesRuntime(data.runtimeRoot, data.hashes), /expected .* pinned source/);
	fs.writeFileSync(data.manifestPath, JSON.stringify({ ...data.manifest, target_arch: 'wrong-arch' }));
	assert.throws(() => verifyHermesRuntime(data.runtimeRoot, data.hashes), /does not match verifier/);
});

test('runtime preparation refuses an invalid copied runtime without deleting the existing one', async (t) => {
	const data = await fixture(t); if (!data) return;
	const { prepareHermesRuntime } = await import('../scripts/hermes-runtime.mjs');
	const output = path.join(data.root, 'output');
	fs.mkdirSync(output);
	fs.writeFileSync(path.join(output, 'keep'), 'existing');
	assert.throws(() => prepareHermesRuntime({ runtimeRoot: output, sourcePath: data.runtimeRoot }), /source verification failed/);
	assert.equal(fs.readFileSync(path.join(output, 'keep'), 'utf8'), 'existing');
	assert.equal(fs.readdirSync(data.root).some(name => name.startsWith('.hermes-build-')), false);
});

test('rejects an imported version that disagrees with the source stamp', async (t) => {
	const data = await fixture(t); if (!data) return;
	const { verifyHermesRuntime } = await import('../scripts/verify-hermes-runtime.mjs');
	const contents = "__version__ = '0.19.0'\n";
	fs.writeFileSync(path.join(data.source, 'hermes_cli/__init__.py'), contents);
	data.hashes['hermes_cli/__init__.py'] = crypto.createHash('sha256').update(contents).digest('hex');
	assert.throws(() => verifyHermesRuntime(data.runtimeRoot, data.hashes), /Hermes version 0\.19\.0; expected/);
});

test('rejects modules imported from outside the bundled source', async (t) => {
	const data = await fixture(t); if (!data) return;
	const { verifyHermesRuntime } = await import('../scripts/verify-hermes-runtime.mjs');
	const external = path.join(data.root, 'external');
	fs.mkdirSync(external);
	fs.cpSync(path.join(data.source, 'hermes_cli'), path.join(external, 'hermes_cli'), { recursive: true });
	if (process.platform === 'win32') {
		fs.appendFileSync(data.importPath, 'import sys; sys.path.insert(0, ' + JSON.stringify(external) + ')\n');
	} else {
		const launcher = fs.readFileSync(data.importPath, 'utf8');
		fs.writeFileSync(data.importPath, launcher.replace('export PYTHONPATH="', 'export PYTHONPATH="$SCRIPT_DIR/../../../external:'), { mode: 0o755 });
	}
	assert.throws(() => verifyHermesRuntime(data.runtimeRoot, data.hashes), /outside bundled source/);
});
