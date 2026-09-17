import { describe, expect, it, vi } from 'vitest';
import { KLine } from './backend';
import { ThemeKLineQueue } from './theme-kline-queue';

const lines = [{ time: '2026-09-17', close: 10 }] as KLine[];
const settle = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };
function controlled() {
	const requests = new Map<string, { resolve: (value: KLine[]) => void; reject: (error: Error) => void; signal: AbortSignal }>();
	const load = vi.fn((symbol: string, signal: AbortSignal) => new Promise<KLine[]>((resolve, reject) => {
		requests.set(symbol, { resolve, reject, signal });
		signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true });
	}));
	return { requests, load };
}

describe('progressive theme K lines', () => {
	it('publishes fast rows while the selected stock is still pending and reserves interactive capacity', async () => {
		const { requests, load } = controlled(); const changed = vi.fn();
		const queue = new ThemeKLineQueue(load, changed);
		queue.setWanted(['a', 'b', 'c', 'd', 'e'], 'a', ['prefetch']);
		expect(load.mock.calls.map(c => c[0])).toEqual(['a', 'b', 'c', 'd']);
		requests.get('b')!.resolve(lines); await settle();
		expect(queue.entries.get('b')?.lines).toEqual(lines);
		expect(queue.entries.get('a')?.pending).toBe(true);
		expect(requests.has('e')).toBe(true);
		expect(requests.has('prefetch')).toBe(false);
		queue.setWanted(['a', 'b', 'c', 'd', 'e', 'new'], 'new');
		// a was the former selected request; abort obsolete work to free its slot.
		requests.get('a')!.resolve(lines); await settle();
		expect(requests.has('new')).toBe(true);
		queue.pause(); await settle();
	});

	it('deduplicates chart and metric consumers, finishes failures, and retries only failed symbols', async () => {
		const { requests, load } = controlled(); const queue = new ThemeKLineQueue(load, () => {});
		queue.setWanted(['a', 'b'], 'a'); queue.setWanted(['a', 'b'], 'a');
		expect(load).toHaveBeenCalledTimes(2);
		requests.get('a')!.resolve(lines); requests.get('b')!.reject(new Error('upstream unavailable')); await settle();
		expect(queue.entries.get('b')).toMatchObject({ pending: false, error: 'upstream unavailable' });
		queue.setWanted(['a', 'b'], 'a'); expect(load).toHaveBeenCalledTimes(2);
		queue.retryFailed(); expect(load.mock.calls.map(c => c[0])).toEqual(['a', 'b', 'b']);
		requests.get('b')!.resolve(lines); await settle(); queue.pause();
	});

	it('ignores responses from a discarded selection and starts the new one without waiting for background rows', async () => {
		const { requests, load } = controlled(); const queue = new ThemeKLineQueue(load, () => {});
		queue.setWanted(['a', 'b', 'c'], '');
		queue.setWanted(['a', 'b', 'c', 'selected'], 'selected');
		expect(requests.has('selected')).toBe(true);
		const old = requests.get('selected')!;
		queue.setWanted(['next'], 'next'); await settle();
		expect(old.signal.aborted).toBe(true);
		old.resolve(lines); await settle();
		expect(queue.entries.get('selected')?.lines).toBeUndefined();
		requests.get('next')!.resolve(lines); await settle(); queue.pause();
	});

	it('expires history across trading dates and a stalled stock reaches a terminal timeout', async () => {
		vi.useFakeTimers();
		try {
			const { requests, load } = controlled(); let day = '2026-09-16';
			const queue = new ThemeKLineQueue(load, () => {}, Date.now, () => day);
			queue.setWanted(['a'], 'a'); requests.get('a')!.resolve(lines); await settle();
			day = '2026-09-17'; queue.setWanted(['a'], 'a');
			expect(load).toHaveBeenCalledTimes(2); expect(queue.entries.get('a')?.lines).toBeUndefined();
			await vi.advanceTimersByTimeAsync(25_000);
			expect(queue.entries.get('a')).toMatchObject({ pending: false, error: '日 K 加载超时' });
			queue.pause();
		} finally { vi.useRealTimers(); }
	});
});
