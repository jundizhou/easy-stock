import { describe, expect, it } from 'vitest';
import type { AppUpdateStatus } from '../lib/backend';
import { updatePrimaryAction } from './AppUpdatePanel';

const status = (state: AppUpdateStatus['state']): AppUpdateStatus => ({
	state,
	supported: true,
	currentVersion: '0.3.0',
	message: '',
	progress: 0,
});

describe('app updater controls', () => {
	it('checks when idle or after an error', () => {
		expect(updatePrimaryAction(status('idle'))).toBe('check');
		expect(updatePrimaryAction(status('error'))).toBe('check');
	});

	it('downloads an available version', () => {
		expect(updatePrimaryAction(status('available'))).toBe('download');
	});

	it('installs only after the download completes', () => {
		expect(updatePrimaryAction(status('downloading'))).toBe('check');
		expect(updatePrimaryAction(status('downloaded'))).toBe('install');
	});
});
