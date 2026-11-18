const assert = require('node:assert/strict');
const path = require('node:path');
const test = require('node:test');

const mountPath = '/build/staging/volume';
const imagePath = '/build/staging/image.dmg';
const helper = import('../scripts/dmg-detach.mjs');

async function fixture(attempts, states, options = {}) {
	const { detachDmg } = await helper;
	const calls = [];
	const waits = [];
	const spawn = (command, args, settings) => {
		if (command === 'plutil') return { status: 0, stdout: settings.input };
		assert.equal(command, 'hdiutil');
		if (args[0] === 'info') {
			assert.ok(states.length, 'unexpected image status probe');
			const state = states.shift();
			if (state instanceof Error) return { error: state };
			return { status: 0, stdout: JSON.stringify({ images: state ? [{
				'image-path': options.reportedPath || imagePath,
				'system-entities': [{ 'dev-entry': state }],
			}] : [] }) };
		}
		calls.push(args);
		assert.ok(attempts.length, 'unexpected detach attempt');
		return attempts.shift();
	};
	return { calls, waits, detach: () => detachDmg(mountPath, imagePath, {
		spawn, realpath: options.realpath || path.resolve,
		wait: async (ms) => { waits.push(ms); },
	}) };
}

test('successful DMG detach verifies the image has disconnected', async () => {
	const f = await fixture([{ status: 0 }], [null]);
	await f.detach();
	assert.deepEqual(f.calls, [['detach', mountPath]]);
});

test('DMG detach accepts disconnection completed after a timeout', async () => {
	const f = await fixture([{ status: 1 }], [null]);
	await f.detach();
	assert.equal(f.calls.length, 1);
});

test('DMG detach ejects the remaining disk even after its filesystem unmounts', async () => {
	const f = await fixture([{ status: 1 }, { status: 0 }], ['/dev/disk5', null]);
	await f.detach();
	assert.deepEqual(f.calls[1], ['detach', '/dev/disk5', '-force']);
});

test('DMG detach accepts a failed command if the image has disconnected', async () => {
	const f = await fixture([{ status: 1 }, { status: 1 }], ['/dev/disk5', null]);
	await f.detach();
});

test('DMG detach rechecks the image after waiting before another forced attempt', async () => {
	const f = await fixture([{ status: 1 }, { status: 1 }, { status: 0 }], ['/dev/disk5', '/dev/disk6', null]);
	await f.detach();
	assert.deepEqual(f.calls.slice(1), [['detach', '/dev/disk5', '-force'], ['detach', '/dev/disk6', '-force']]);
	assert.deepEqual(f.waits, [1000, 1000]);
});

test('DMG detach rejects an image still attached after bounded retries', async () => {
	const f = await fixture(Array.from({ length: 4 }, () => ({ status: 1 })), Array(4).fill('/dev/disk5'));
	await assert.rejects(f.detach(), /still attached/);
	assert.equal(f.calls.length, 4);
});

test('DMG detach resolves macOS path aliases before matching the image', async () => {
	const f = await fixture([{ status: 1 }, { status: 0 }], ['/dev/disk5', null], {
		reportedPath: '/private/build/staging/image.dmg',
		realpath: (value) => value.replace(/^\/private/, ''),
	});
	await f.detach();
	assert.deepEqual(f.calls[1], ['detach', '/dev/disk5', '-force']);
});

test('DMG detach preserves command and image status errors', async () => {
	const f = await fixture([{ status: 1 }], [new Error('image status unavailable')]);
	await assert.rejects(f.detach(), /image status unavailable/);
	const failed = await fixture([{ error: new Error('cannot execute hdiutil') }], []);
	await assert.rejects(failed.detach(), /cannot execute hdiutil/);
});

test('DMG detach never forces an unrelated disk image', async () => {
	const f = await fixture([{ status: 1 }], ['/dev/disk99'], { reportedPath: '/unrelated/image.dmg' });
	await f.detach();
	assert.deepEqual(f.calls, [['detach', mountPath]]);
});

test('DMG detach fails closed if its backing device cannot be identified', async () => {
	const f = await fixture([{ status: 1 }], ['/dev/disk5s1']);
	await assert.rejects(f.detach(), /Cannot identify/);
	assert.equal(f.calls.length, 1);
});
