import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

export const HERMES_AGENT_VERSION = process.env.HERMES_AGENT_VERSION || '0.18.2';

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
	fs.rmSync(runtimeRoot, { recursive: true, force: true });
	fs.mkdirSync(path.dirname(runtimeRoot), { recursive: true });

	let mode = 'installed';
	if (sourcePath) {
		const resolvedSource = path.resolve(sourcePath);
		if (!fs.existsSync(resolvedSource)) throw new Error(`Hermes runtime source not found: ${resolvedSource}`);
		fs.cpSync(resolvedSource, runtimeRoot, { recursive: true });
		rewriteCopiedSymlinks(runtimeRoot, resolvedSource);
		mode = 'copied';
	} else {
		if (platform !== process.platform || arch !== process.arch) {
			throw new Error('Hermes runtime must be prepared on the target platform and architecture');
		}
		fs.mkdirSync(runtimeRoot, { recursive: true });
		run(uv, ['venv', path.join(runtimeRoot, 'venv'), '--python', process.env.HERMES_RUNTIME_PYTHON || '3.11', '--managed-python', '--relocatable', '--link-mode', 'copy'], runtimeRoot);
		const venvPython = hermesVenvPython(runtimeRoot, platform);
		run(uv, ['pip', 'install', '--python', venvPython, '--link-mode', 'copy', `hermes-agent[all]==${HERMES_AGENT_VERSION}`], runtimeRoot);
		vendorRuntimePython(runtimeRoot, platform);
	}

	removePythonCaches(runtimeRoot);
	assertNoExternalSymlinks(runtimeRoot);
	materializeSymlinks(runtimeRoot);
	const python = hermesRuntimePython(runtimeRoot, platform);
	if (!fs.existsSync(python)) throw new Error(`Hermes runtime Python not found: ${python}`);
	run(python, ['-c', 'import hermes_cli, tui_gateway'], runtimeRoot, { PYTHONNOUSERSITE: '1' });
	const installedVersion = readHermesVersion(python, runtimeRoot);
	const manifest = {
		schema_version: 1,
		package: 'hermes-agent',
		version: installedVersion,
		mode,
		target_platform: platform,
		target_arch: arch,
		created_at: new Date().toISOString(),
	};
	if (installedVersion !== HERMES_AGENT_VERSION) manifest.requested_version = HERMES_AGENT_VERSION;
	fs.writeFileSync(path.join(runtimeRoot, 'runtime-manifest.json'), `${JSON.stringify(manifest, null, 2)}\n`);
	return manifest;
}

function readHermesVersion(python, cwd) {
	const script = 'import importlib.metadata as metadata; print(metadata.version("hermes-agent"))';
	const result = spawnSync(python, ['-c', script], {
		cwd,
		encoding: 'utf8',
		env: { ...process.env, PYTHONNOUSERSITE: '1' },
	});
	if (result.error) throw result.error;
	if (result.status !== 0) {
		throw new Error(`Unable to read Hermes version: ${(result.stderr || '').trim() || `python exited with status ${result.status}`}`);
	}
	const version = (result.stdout || '').trim();
	if (!version) throw new Error('Hermes runtime did not report an installed version');
	return version;
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
	for (const alias of ['python3', 'python3.11']) {
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
	const sourcePython = path.join(sourceRoot, 'python.exe');
	const sourceSitePackages = path.join(runtimeRoot, 'venv', 'Lib', 'site-packages');
	if (!fs.existsSync(sourcePython)) throw new Error(`Managed Windows Python executable not found: ${sourcePython}`);
	if (!fs.existsSync(sourceSitePackages)) throw new Error(`Hermes virtual environment site-packages not found: ${sourceSitePackages}`);

	const bundledRoot = path.join(runtimeRoot, 'python');
	fs.rmSync(bundledRoot, { recursive: true, force: true });
	fs.cpSync(sourceRoot, bundledRoot, { recursive: true });
	rewriteCopiedSymlinks(bundledRoot, sourceRoot);

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
	const pythonRelative = path.relative(binRoot, target).split(path.sep).join('/');
	const sitePackagesRelative = path.relative(binRoot, path.join(libRoot, pythonLib.name, 'site-packages')).split(path.sep).join('/');
	const script = [
		'#!/bin/sh',
		'SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)',
		'if [ -n "$PYTHONPATH" ]; then',
		'  PYTHONPATH="$SCRIPT_DIR/' + sitePackagesRelative + ':$PYTHONPATH"',
		'else',
		'  PYTHONPATH="$SCRIPT_DIR/' + sitePackagesRelative + '"',
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
