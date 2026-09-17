import { afterEach, describe, expect, it, vi } from 'vitest';
import { requestJSON } from './backend';
import { pollShortTermProgress, ShortTermProgress } from './short-term-progress';

vi.mock('./backend', () => ({ requestJSON: vi.fn() }));
const request = vi.mocked(requestJSON);
const config = { backendUrl: 'http://localhost:20000', token: '' };
const value = (refreshing: boolean): ShortTermProgress<string> => ({ data: '基础梯队', refresh_id: 'batch-1', revision: refreshing ? 1 : 2, refreshing, stale: false, steps: {}, errors: refreshing ? {} : { concepts: '目录不可用' } });
afterEach(() => { vi.useRealTimers(); vi.resetAllMocks(); });

describe('short term progressive polling', () => {
	it('accepts a complete response from an older backend awaiting restart', async () => {
		request.mockResolvedValue({ data: '完整梯队' });
		const publish = vi.fn();
		await pollShortTermProgress(config, '/ladder', publish, { signal: new AbortController().signal, refresh: false, budgetMS: 500 });
		expect(publish).toHaveBeenCalledWith({ data: '完整梯队', refresh_id: '', revision: 0, refreshing: false, stale: false, steps: {}, errors: {} });
	});
	it('publishes partial data immediately and polls without restarting the refresh', async () => {
		vi.useFakeTimers();
		request.mockResolvedValueOnce(value(true)).mockResolvedValueOnce(value(false));
		const publish = vi.fn();
		const operation = pollShortTermProgress(config, '/ladder', publish, { signal: new AbortController().signal, refresh: true, budgetMS: 35000 });
		await vi.advanceTimersByTimeAsync(0);
		expect(publish).toHaveBeenCalledWith(value(true));
		await vi.advanceTimersByTimeAsync(250); await operation;
		expect(request.mock.calls[0][1]).toContain('refresh=1');
		expect(request.mock.calls[1][1]).toContain('refresh_id=batch-1');
		expect(request.mock.calls[1][1]).not.toContain('refresh=1');
		expect(publish).toHaveBeenLastCalledWith(value(false));
		await vi.advanceTimersByTimeAsync(10000); expect(request).toHaveBeenCalledTimes(2);
	});
	it('ignores late responses after leaving the page even when transport ignores abort', async () => {
		let resolve!: (value: unknown) => void;
		request.mockImplementation(() => new Promise(done => { resolve = done; }));
		const abort = new AbortController(); const publish = vi.fn();
		const operation = pollShortTermProgress(config, '/ladder', publish, { signal: abort.signal, refresh: false, budgetMS: 35000 });
		abort.abort(); await operation;
		resolve(value(true)); await Promise.resolve();
		expect(publish).not.toHaveBeenCalled(); expect(request).toHaveBeenCalledTimes(1);
	});
	it('a hung request reaches a terminal timeout', async () => {
		vi.useFakeTimers(); request.mockImplementation(() => new Promise(() => {}));
		const operation = pollShortTermProgress(config, '/history', vi.fn(), { signal: new AbortController().signal, refresh: false, budgetMS: 500 });
		const check = expect(operation).rejects.toThrow('数据更新超时');
		await vi.advanceTimersByTimeAsync(500); await check;
		expect(request).toHaveBeenCalledTimes(1);
	});
});
