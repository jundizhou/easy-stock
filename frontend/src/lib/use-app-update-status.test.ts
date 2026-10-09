import { describe, expect, it } from 'vitest';
import type { AppUpdateStatus } from './backend';
import { hasNewAppVersion } from './use-app-update-status';

const available: AppUpdateStatus = {
	state: 'available', supported: true, currentVersion: '1.3.1', latestVersion: '1.3.2', message: '', progress: 0,
};

describe('new version notice', () => {
	it('shows an eligible update while checking, downloading or retrying a failed download', () => {
		for (const state of ['available', 'checking', 'downloading', 'downloaded', 'installing', 'error'] as const) {
			expect(hasNewAppVersion({ ...available, state })).toBe(true);
		}
		expect(hasNewAppVersion({ ...available, installMode: 'manual' })).toBe(true);
	});

	it('hides the notice without a newer version or in an unsupported environment', () => {
		for (const latestVersion of [undefined, '1.3.1', '1.2.9', 'invalid']) {
			expect(hasNewAppVersion({ ...available, latestVersion })).toBe(false);
		}
		expect(hasNewAppVersion({ ...available, supported: false })).toBe(false);
		expect(hasNewAppVersion({ ...available, state: 'disabled' })).toBe(false);
	});

	it('compares version numbers numerically and accepts the release tag prefix', () => {
		expect(hasNewAppVersion({ ...available, latestVersion: 'v1.10.0' })).toBe(true);
		expect(hasNewAppVersion({ ...available, currentVersion: '1.10.0', latestVersion: '1.9.9' })).toBe(false);
		expect(hasNewAppVersion({ ...available, currentVersion: 'v1.3.1', latestVersion: '1.3.1' })).toBe(false);
		expect(hasNewAppVersion({ ...available, currentVersion: '1.3.1+build.1', latestVersion: '1.3.1+build.2' })).toBe(false);
	});

	it('recognizes a stable release replacing a prerelease of the same version', () => {
		expect(hasNewAppVersion({ ...available, currentVersion: '1.3.2-beta.1' })).toBe(true);
		expect(hasNewAppVersion({ ...available, currentVersion: '1.3.2', latestVersion: '1.3.2-beta.1' })).toBe(false);
	});
});
