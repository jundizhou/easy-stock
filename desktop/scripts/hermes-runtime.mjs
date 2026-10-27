import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';

export const HERMES_AGENT_VERSION = '0.22.5';
export const HERMES_AGENT_TAG = 'v0.22.5';
export const HERMES_AGENT_SOURCE = `https://codeload.github.com/NousResearch/hermes-agent/tar.gz/refs/tags/${HERMES_AGENT_TAG}`;
export const HERMES_SOURCE_SHA256 = 'a8f67e5ec14fea681acef6cc7ff1553f002bb6101fd2df22bfdd4b7eecc54eb2';
export const HERMES_SOURCE_FILES = {
	'hermes_cli/__init__.py': '6a4ab05b8621ec1cd66a5abbe30fa32ed5bddddea95903036da01f0af1af94ce',
	'tui_gateway/entry.py': 'e58c125e3b5d7b2cfd310cd9d63f77590baa170cba2eb7ce589eb7e7e82b5736',
	'pyproject.toml': '68325d6717bd4ebc3a7b015c5150bea4d5600c612259b9b7393e964caa828761',
};

export function hermesRuntimePython(runtimeRoot, platform = process.platform) {
	return platform === 'win32'
		? path.join(runtimeRoot, 'python', 'python.exe')
		: path.join(runtimeRoot, 'venv', 'bin', 'python');
}

export function hermesVenvPython(runtimeRoot, platform = process.platform) {
	return platform === 'win32'
		? path.join(runtimeRoot, 'venv', 'Scripts', 'python.exe')
		: path.join(runtimeRoot, 'venv', 'bin', 'python');
}

export function prepareHermesRuntime({
	runtimeRoot,
	sourcePath = process.env.HERMES_RUNTIME_SOURCE || process.env.A_STOCK_HERMES_RUNTIME_SOURCE || '',
	platform = process.platform,
	arch = process.arch,
	uv = process.env.UV || 'uv',
} = {}) {
	if (!runtimeRoot) throw new Error('runtimeRoot is required');
	runtimeRoot = path.resolve(runtimeRoot);
	fs.mkdirSync(path.dirname(runtimeRoot), { recursive: true });
	const staging = fs.mkdtempSync(path.join(path.dirname(runtimeRoot), '.hermes-build-'));
	const builtRoot = path.join(staging, 'runtime');
	let mode = 'installed';
	try {
		if (sourcePath) {
			const resolvedSource = path.resolve(sourcePath);
			if (!fs.existsSync(resolvedSource)) throw new Error(`Hermes runtime source not found: ${resolvedSource}`);
			fs.cpSync(resolvedSource, builtRoot, { recursive: true });
			rewriteCopiedSymlinks(builtRoot, resolvedSource);
			mode = 'copied';
		} else {
			if (platform !== process.platform || arch !== process.arch) throw new Error('Hermes runtime must be prepared on the target platform and architecture');
			fs.mkdirSync(builtRoot, { recursive: true });
			const archive = path.join(staging, 'hermes.tar.gz');
			if (process.env.HERMES_AGENT_ARCHIVE) fs.copyFileSync(process.env.HERMES_AGENT_ARCHIVE, archive);
			else run('curl', ['-fsSL', '--retry', '4', '--retry-delay', '2', '--retry-all-errors', HERMES_AGENT_SOURCE, '-o', archive], staging);
			const digest = createHash('sha256').update(fs.readFileSync(archive)).digest('hex');
			if (digest !== HERMES_SOURCE_SHA256) throw new Error('Hermes source archive checksum mismatch');
			const listing = spawnSync('tar', ['-tzf', archive], { encoding: 'utf8', maxBuffer: 32 * 1024 * 1024 });
			if (listing.status !== 0 || listing.stdout.split('\n').filter(Boolean).some((entry) => entry.startsWith('/') || entry.includes('\\') || entry.split('/').includes('..'))) throw new Error('Unsafe Hermes source archive');
			const sourceRoot = path.join(builtRoot, 'source');
			fs.mkdirSync(sourceRoot);
			run('tar', ['-xzf', archive, '-C', sourceRoot, '--strip-components=1'], staging);
			assertNoExternalSymlinks(sourceRoot);
			const stamp = { schemaVersion: 2, commit: 'bf0203a794a', baseVersion: HERMES_AGENT_VERSION, displayVersion: HERMES_AGENT_VERSION, distance: 0, source: 'build', distribution: 'desktop-app', updateMechanism: 'external', payload: 'bootstrap' };
			fs.writeFileSync(path.join(sourceRoot, 'install-stamp.json'), JSON.stringify(stamp) + '\n');
			run(uv, ['venv', path.join(builtRoot, 'venv'), '--python', '3.14', '--managed-python', '--relocatable', '--link-mode', 'copy'], staging);
			const venvPython = hermesVenvPython(builtRoot, platform);
			const requirements = path.join(staging, 'requirements.txt');
			run(uv, ['export', '--locked', '--no-emit-project', '--no-dev', '--extra', 'all', '--extra', 'anthropic', '--format', 'requirements-txt', '--output-file', requirements, '--quiet'], sourceRoot);
			// The lock contains intentional overrides (e.g. Pillow); install the
			// complete pinned set without re-resolving those upstream overrides.
			run(uv, ['pip', 'install', '--python', venvPython, '--link-mode', 'copy', '--no-deps', '--require-hashes', '-r', requirements], staging);
			if (platform === 'win32') {
				const site = path.join(builtRoot, 'venv', 'Lib', 'site-packages');
				fs.writeFileSync(path.join(site, 'hermes-source.pth'), '../../../source\n');
			}
			vendorRuntimePython(builtRoot, platform);
		}
		removePythonCaches(builtRoot);
		assertNoExternalSymlinks(builtRoot);
		materializeSymlinks(builtRoot);
		const manifest = { schema_version: 2, package: 'hermes-agent', version: HERMES_AGENT_VERSION, source: HERMES_AGENT_SOURCE, source_sha256: HERMES_SOURCE_SHA256, mode, target_platform: platform, target_arch: arch, created_at: new Date().toISOString() };
		fs.writeFileSync(path.join(builtRoot, 'runtime-manifest.json'), `${JSON.stringify(manifest, null, 2)}\n`);
		verifyHermesPython(builtRoot);
		const previous = path.join(staging, 'previous');
		const hadPrevious = fs.existsSync(runtimeRoot);
		if (hadPrevious) fs.renameSync(runtimeRoot, previous);
		try {
			fs.renameSync(builtRoot, runtimeRoot);
		} catch (error) {
			if (hadPrevious) fs.renameSync(previous, runtimeRoot);
			throw error;
		}
		return manifest;
	} finally {
		fs.rmSync(staging, { recursive: true, force: true });
	}
}

export function verifyHermesSource(runtimeRoot, expectedFiles = HERMES_SOURCE_FILES) {
	for (const [name, expected] of Object.entries(expectedFiles)) {
		const file = path.join(runtimeRoot, 'source', name);
		if (!fs.existsSync(file) || createHash('sha256').update(fs.readFileSync(file)).digest('hex') !== expected) throw new Error(`Hermes source verification failed: ${name}`);
	}
	const stamp = JSON.parse(fs.readFileSync(path.join(runtimeRoot, 'source', 'install-stamp.json'), 'utf8'));
	if (stamp.baseVersion !== HERMES_AGENT_VERSION) throw new Error(`Hermes source version ${stamp.baseVersion}; expected ${HERMES_AGENT_VERSION}`);
	return stamp.baseVersion;
}

export function verifyHermesPython(runtimeRoot, expectedFiles = HERMES_SOURCE_FILES) {
	verifyHermesSource(runtimeRoot, expectedFiles);
	const script = `
import json, sys
from pathlib import Path
output = sys.stdout
import hermes_cli, tui_gateway, tui_gateway.entry
root = Path(sys.argv[1]).resolve() / "source"
for module in (hermes_cli, tui_gateway, tui_gateway.entry):
    if not Path(module.__file__).resolve().is_relative_to(root):
        raise RuntimeError(f"Hermes module outside bundled source: {module.__file__}")
if sys.version_info[:2] != (3, 14):
    raise RuntimeError(f"Hermes requires Python 3.14; found {sys.version}")
if hermes_cli.__version__ != sys.argv[2]:
    raise RuntimeError(f"Hermes version {hermes_cli.__version__}; expected {sys.argv[2]}")
print(json.dumps({"version": hermes_cli.__version__}), file=output)
`;
	const env = { ...process.env, PYTHONNOUSERSITE: '1', PYTHONDONTWRITEBYTECODE: '1' };
	delete env.PYTHONPATH;
	delete env.PYTHONHOME;
	delete env.VIRTUAL_ENV;
	const args = [...(process.platform === 'win32' ? ['-I'] : []), '-c', script, path.resolve(runtimeRoot), HERMES_AGENT_VERSION];
	const result = spawnSync(hermesRuntimePython(runtimeRoot), args, { cwd: runtimeRoot, encoding: 'utf8', env, timeout: 60_000 });
	if (result.error) throw result.error;
	if (result.status !== 0) throw new Error(`Bundled Hermes verification failed: ${(result.stderr || result.stdout || '').trim() || `exit status ${result.status}`}`);
	return JSON.parse(result.stdout.trim());
}

function vendorRuntimePython(runtimeRoot, platform) {
	if (platform === 'win32') {
		const venvPython = hermesVenvPython(runtimeRoot, platform);
		const sourceRoot = readPythonBasePrefix(venvPython, runtimeRoot);
		bundleWindowsRuntime(runtimeRoot, sourceRoot);
		return;
	}
	const python = hermesRuntimePython(runtimeRoot, platform);
	let stat;
	try {
		stat = fs.lstatSync(python);
	} catch {
		return;
	}
	if (!stat.isSymbolicLink()) return;
	const sourcePython = fs.realpathSync(python);
	const sourceRoot = path.dirname(path.dirname(sourcePython));
	const bundledRoot = path.join(runtimeRoot, 'python');
	fs.rmSync(bundledRoot, { recursive: true, force: true });
	fs.cpSync(sourceRoot, bundledRoot, { recursive: true });
	rewriteCopiedSymlinks(bundledRoot, sourceRoot);
	fs.rmSync(python, { force: true });
	fs.symlinkSync(path.relative(path.dirname(python), path.join(bundledRoot, 'bin', path.basename(sourcePython))), python);
	for (const alias of ['python3', 'python3.14']) {
		const aliasPath = path.join(path.dirname(python), alias);
		fs.rmSync(aliasPath, { force: true });
		fs.symlinkSync('python', aliasPath);
	}
}

function readPythonBasePrefix(python, cwd) {
	const result = spawnSync(python, ['-I', '-c', 'import sys; print(sys.base_prefix)'], {
		cwd,
		encoding: 'utf8',
		env: { ...process.env, PYTHONNOUSERSITE: '1' },
	});
	if (result.error) throw result.error;
	if (result.status !== 0) {
		throw new Error(`Unable to locate the managed Python runtime: ${(result.stderr || '').trim() || `python exited with status ${result.status}`}`);
	}
	const reportedRoot = (result.stdout || '').trim();
	if (!reportedRoot) throw new Error('Managed Python runtime did not report sys.base_prefix');
	const sourceRoot = path.resolve(reportedRoot);
	if (!fs.existsSync(sourceRoot)) throw new Error(`Managed Python runtime not found: ${sourceRoot}`);
	return sourceRoot;
}

export function bundleWindowsRuntime(runtimeRoot, sourceRoot) {
	const sourceRealRoot = fs.realpathSync(sourceRoot);
	const sourcePython = path.join(sourceRealRoot, 'python.exe');
	const sourceSitePackages = path.join(runtimeRoot, 'venv', 'Lib', 'site-packages');
	if (!fs.existsSync(sourcePython)) throw new Error(`Managed Windows Python executable not found: ${sourcePython}`);
	if (!fs.existsSync(sourceSitePackages)) throw new Error(`Hermes virtual environment site-packages not found: ${sourceSitePackages}`);

	const bundledRoot = path.join(runtimeRoot, 'python');
	fs.rmSync(bundledRoot, { recursive: true, force: true });
	fs.cpSync(sourceRealRoot, bundledRoot, { recursive: true, dereference: true });

	const bundledSitePackages = path.join(bundledRoot, 'Lib', 'site-packages');
	fs.mkdirSync(bundledSitePackages, { recursive: true });
	fs.cpSync(sourceSitePackages, bundledSitePackages, { recursive: true, force: true });

	// A Windows venv launcher keeps an absolute `home` path in pyvenv.cfg.
	// Removing the build-only venv ensures the shipped runtime cannot silently
	// depend on the GitHub Actions runner's uv-managed Python installation.
	fs.rmSync(path.join(runtimeRoot, 'venv'), { recursive: true, force: true });
}

function assertNoExternalSymlinks(runtimeRoot) {
	const root = path.resolve(runtimeRoot);
	walk(runtimeRoot, (entryPath) => {
		let stat;
		try {
			stat = fs.lstatSync(entryPath);
		} catch {
			return;
		}
		if (!stat.isSymbolicLink()) return;
		const target = path.resolve(path.dirname(entryPath), fs.readlinkSync(entryPath));
		const relative = path.relative(root, target);
		if (relative.startsWith('..') || path.isAbsolute(relative)) {
			throw new Error(`Hermes runtime contains an external symlink: ${entryPath} -> ${target}`);
		}
	});
}

function materializeSymlinks(runtimeRoot) {
	for (;;) {
		let count = 0;
		walk(runtimeRoot, (entryPath) => {
			let stat;
			try {
				stat = fs.lstatSync(entryPath);
			} catch {
				return;
			}
			if (!stat.isSymbolicLink()) return;
			const target = fs.realpathSync(entryPath);
			if (isVenvPythonLauncher(runtimeRoot, entryPath)) {
				writeVenvPythonLauncher(runtimeRoot, entryPath, target);
				count += 1;
				return;
			}
			const replacement = `${entryPath}.materialized`;
			fs.rmSync(replacement, { recursive: true, force: true });
			if (fs.statSync(target).isDirectory()) {
				fs.cpSync(target, replacement, { recursive: true });
			} else {
				fs.copyFileSync(target, replacement);
				fs.chmodSync(replacement, fs.statSync(target).mode);
			}
			fs.rmSync(entryPath, { recursive: true, force: true });
			fs.renameSync(replacement, entryPath);
			count += 1;
		});
		if (count === 0) return;
	}
}

function isVenvPythonLauncher(runtimeRoot, entryPath) {
	const relative = path.relative(path.join(runtimeRoot, 'venv', 'bin'), entryPath);
	return relative !== '' && !relative.includes(path.sep) && /^python(?:3(?:\.\d+)?)?$/.test(relative);
}

function writeVenvPythonLauncher(runtimeRoot, entryPath, target) {
	const binRoot = path.dirname(entryPath);
	const libRoot = path.join(runtimeRoot, 'venv', 'lib');
	const pythonLib = fs.readdirSync(libRoot, { withFileTypes: true }).find((entry) => (
		entry.isDirectory() && entry.name.startsWith('python') && fs.existsSync(path.join(libRoot, entry.name, 'site-packages'))
	));
	if (!pythonLib) throw new Error('Hermes virtual environment site-packages directory not found');
	const pythonRelative = path.relative(fs.realpathSync(binRoot), target).split(path.sep).join('/');
	const sitePackagesRelative = path.relative(binRoot, path.join(libRoot, pythonLib.name, 'site-packages')).split(path.sep).join('/');
	const sourceRelative = path.relative(binRoot, path.join(runtimeRoot, 'source')).split(path.sep).join('/');
	const script = [
		'#!/bin/sh',
		'SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)',
		'if [ -n "$PYTHONPATH" ]; then',
		'  PYTHONPATH="$SCRIPT_DIR/' + sourceRelative + ':$SCRIPT_DIR/' + sitePackagesRelative + ':$PYTHONPATH"',
		'else',
		'  PYTHONPATH="$SCRIPT_DIR/' + sourceRelative + ':$SCRIPT_DIR/' + sitePackagesRelative + '"',
		'fi',
		'export PYTHONPATH',
		'exec "$SCRIPT_DIR/' + pythonRelative + '" "$@"',
		'',
	].join('\n');
	fs.rmSync(entryPath, { force: true });
	fs.writeFileSync(entryPath, script, { mode: 0o755 });
}

function rewriteCopiedSymlinks(runtimeRoot, sourceRoot) {
	const sourceReal = fs.realpathSync(sourceRoot);
	walk(runtimeRoot, (entryPath) => {
		let stat;
		try {
			stat = fs.lstatSync(entryPath);
		} catch {
			return;
		}
		if (!stat.isSymbolicLink()) return;
		const link = fs.readlinkSync(entryPath);
		const absoluteTarget = path.resolve(path.dirname(entryPath), link);
		const roots = [path.resolve(sourceRoot), sourceReal];
		for (const root of roots) {
			const relative = path.relative(root, absoluteTarget);
			if (relative === '' || (!relative.startsWith('..') && !path.isAbsolute(relative))) {
				const copiedTarget = path.join(runtimeRoot, relative);
				fs.rmSync(entryPath, { force: true });
				fs.symlinkSync(path.relative(path.dirname(entryPath), copiedTarget), entryPath);
				break;
			}
		}
	});
}

function removePythonCaches(root) {
	walk(root, (entryPath, entry) => {
		if (entry?.isDirectory() && entry.name === '__pycache__') {
			fs.rmSync(entryPath, { recursive: true, force: true });
		} else if (entry?.isFile() && entry.name.endsWith('.pyc')) {
			fs.rmSync(entryPath, { force: true });
		}
	});
}

function walk(root, visit) {
	if (!fs.existsSync(root)) return;
	for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
		const entryPath = path.join(root, entry.name);
		visit(entryPath, entry);
		if (entry.isDirectory() && fs.existsSync(entryPath)) walk(entryPath, visit);
	}
}

function run(command, args, cwd, env = {}) {
	const result = spawnSync(command, args, { cwd, stdio: 'inherit', env: { ...process.env, ...env } });
	if (result.error) throw result.error;
	if (result.status !== 0) throw new Error(`${command} exited with status ${result.status}`);
}
