import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

export async function detachDmg(mountPath, imagePath, {
	spawn = spawnSync,
	realpath = fs.realpathSync,
	wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
} = {}) {
	const wantedImage = realpath(imagePath);
	const attachedDevice = () => {
		const info = spawn('hdiutil', ['info', '-plist'], { encoding: 'utf8' });
		check(info, 'hdiutil info');
		const parsed = spawn('plutil', ['-convert', 'json', '-o', '-', '-'], { input: info.stdout, encoding: 'utf8' });
		check(parsed, 'plutil');
		const { images } = JSON.parse(parsed.stdout);
		if (!Array.isArray(images)) throw new Error('Invalid hdiutil image list');
		const image = images.find((entry) => {
			if (typeof entry['image-path'] !== 'string') return false;
			try { return realpath(entry['image-path']) === wantedImage; }
			catch { return path.resolve(entry['image-path']) === wantedImage; }
		});
		if (!image) return undefined;
		const device = image['system-entities']?.find((entry) => /^\/dev\/disk\d+$/.test(entry['dev-entry'] || ''))?.['dev-entry'];
		if (!device) throw new Error('Cannot identify the temporary DMG device');
		return device;
	};
	let result = spawn('hdiutil', ['detach', mountPath], { stdio: 'inherit' });
	if (result.error) throw result.error;
	for (let attempt = 0; attempt < 3; attempt++) {
		if (attempt > 0) await wait(1000);
		// A filesystem can be unmounted while its disk image is still attached.
		// Resolve our image again before every force attempt, including retries.
		const device = attachedDevice();
		if (!device) return;
		result = spawn('hdiutil', ['detach', device, '-force'], { stdio: 'inherit' });
		if (result.error) throw result.error;
	}
	if (attachedDevice()) throw new Error(`Temporary DMG is still attached after detach (status ${result.status ?? 1})`);
}

function check(result, command) {
	if (result.error) throw result.error;
	if (result.status !== 0) throw new Error(`${command} exited with status ${result.status ?? 1}`);
}
