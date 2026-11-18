const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const source = fs.readFileSync(path.resolve(__dirname, '..', 'scripts', 'create-dmg.mjs'), 'utf8');
const mountPath = '/build/staging/volume';

function fixture(attempts, devices) {
	const calls = [];
	const detach = vm.runInNewContext(source.slice(source.indexOf('\nfunction detach(')) + '\ndetach;', {
		path,
		fs: { statSync: (file) => {
			assert.ok([mountPath, path.dirname(mountPath)].includes(file));
			const device = devices.shift();
			if (device instanceof Error) throw device;
			assert.notEqual(device, undefined, 'unexpected filesystem probe');
			return { dev: device };
		} },
		spawnSync: (command, args) => {
			calls.push({ command, args: Array.from(args) });
			assert.ok(attempts.length, 'unexpected detach attempt');
			return attempts.shift();
		},
	});
	return { calls, detach: () => detach(mountPath) };
}

test('successful DMG detach needs no filesystem probe or forced retry', () => {
	const f = fixture([{ status: 0 }], []);
	f.detach();
	assert.deepEqual(f.calls, [{ command: 'hdiutil', args: ['detach', mountPath] }]);
});

test('DMG detach accepts an unmount completed after the command timed out', () => {
	const f = fixture([{ status: 1 }], [1, 1]);
	f.detach();
	assert.equal(f.calls.length, 1);
});

test('DMG detach accepts a volume that disappears before the forced retry', () => {
	const f = fixture([{ status: 1 }, { status: 1 }], [2, 1, 1, 1]);
	f.detach();
	assert.deepEqual(f.calls[1], { command: 'hdiutil', args: ['detach', mountPath, '-force'] });
});

test('DMG detach still rejects a volume left mounted after both attempts', () => {
	const f = fixture([{ status: 1 }, { status: 1 }], [2, 1, 2, 1]);
	assert.throws(f.detach, /hdiutil detach exited with status 1/);
});

test('DMG detach accepts a removed mount directory', () => {
	const missing = Object.assign(new Error('missing volume'), { code: 'ENOENT' });
	const f = fixture([{ status: 1 }], [missing]);
	f.detach();
	assert.equal(f.calls.length, 1);
});

test('DMG detach preserves filesystem and command errors', () => {
	const denied = Object.assign(new Error('permission denied'), { code: 'EACCES' });
	assert.throws(fixture([{ status: 1 }], [denied]).detach, /permission denied/);
	assert.throws(fixture([{ error: new Error('cannot execute hdiutil') }], []).detach, /cannot execute hdiutil/);
});
