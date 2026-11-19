// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { requestJSON } from './backend';
import { loadETFSubscriptionSnapshot, readETFSubscriptionSnapshot } from './etf-subscription-cache';

vi.mock('./backend', async (original) => ({ ...await original<typeof import('./backend')>(), requestJSON: vi.fn() }));
const config = { backendUrl: 'http://backend.test', token: 'test-token' };
const snapshot = { data: { expected: 22, latest_complete_date: '2026-11-17', days: [], series: [], checks: [], warnings: [] }, meta: { fetched_at: '2026-11-18T14:00:00+08:00', stale: false } };
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date(snapshot.meta.fetched_at)); window.localStorage.clear(); vi.mocked(requestJSON).mockReset(); });
afterEach(() => { vi.useRealTimers(); });

describe('ETF subscription fast-open snapshot', () => {
	it('saves public data without credentials and marks the initial snapshot stale', async () => {
		vi.mocked(requestJSON).mockResolvedValueOnce(snapshot);
		await loadETFSubscriptionSnapshot(config);
		expect(readETFSubscriptionSnapshot(config)?.meta.stale).toBe(true);
		expect(readETFSubscriptionSnapshot({ ...config, backendUrl: 'http://other.test' })).toBeNull();
		expect(JSON.stringify(window.localStorage)).not.toContain('test-token');
		vi.advanceTimersByTime(8 * 24 * 60 * 60 * 1000);
		expect(readETFSubscriptionSnapshot(config)).toBeNull();
	});
	it('deduplicates concurrent fetches and permits a retry after failure', async () => {
		let fail!: (reason: Error) => void;
		vi.mocked(requestJSON).mockImplementationOnce(() => new Promise((_resolve, reject) => { fail = reject; }));
		const first = loadETFSubscriptionSnapshot(config);
		const second = loadETFSubscriptionSnapshot(config);
		expect(first).toBe(second);
		const caught = first.catch((error: Error) => error.message);
		fail(new Error('network failed'));
		expect(await caught).toBe('network failed');
		vi.mocked(requestJSON).mockResolvedValueOnce(snapshot);
		await loadETFSubscriptionSnapshot(config);
		expect(requestJSON).toHaveBeenCalledTimes(2);
	});
	it('ignores corrupt snapshots and storage failures', async () => {
		window.localStorage.setItem('easy-stock:etf-subscriptions:v2:http://backend.test', '{broken');
		expect(readETFSubscriptionSnapshot(config)).toBeNull();
		window.localStorage.setItem('easy-stock:etf-subscriptions:v2:http://backend.test', JSON.stringify({ ...snapshot, data: { ...snapshot.data, days: [null] } }));
		expect(readETFSubscriptionSnapshot(config)).toBeNull();
		vi.spyOn(Storage.prototype, 'setItem').mockImplementationOnce(() => { throw new Error('quota'); });
		vi.mocked(requestJSON).mockResolvedValueOnce(snapshot);
		expect(await loadETFSubscriptionSnapshot(config)).toBe(snapshot);
		vi.restoreAllMocks();
	});
	it('ignores old one-month snapshots after the year-history upgrade', () => {
		window.localStorage.setItem('easy-stock:etf-subscriptions:v1:http://backend.test', JSON.stringify(snapshot));
		expect(readETFSubscriptionSnapshot(config)).toBeNull();
	});
});
