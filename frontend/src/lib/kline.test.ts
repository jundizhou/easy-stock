import { describe, expect, it } from 'vitest';
import { KLine } from './backend';
import { latestTradingDayKLines } from './kline';

function line(time: string, close: number, previousClose?: number): KLine {
	return {
		symbol: '300684.SZ',
		time,
		open: close,
		high: close,
		low: close,
		close,
		previous_close: previousClose,
		volume: 1,
		amount: 1,
		meta: { source: 'test', fetched_at: '', latency_ms: 0, stale: false },
	};
}

describe('latestTradingDayKLines', () => {
	it('removes prior trading-day bars and preserves their close as the baseline', () => {
		const result = latestTradingDayKLines([
			line('2026-08-14T14:50:00+08:00', 67.36),
			line('2026-08-14T15:00:00+08:00', 67.36),
			line('2026-08-17T09:30:00+08:00', 80.83),
			line('2026-08-17T15:00:00+08:00', 80.83),
		]);

		expect(result).toHaveLength(2);
		expect(result.map((item) => item.time)).toEqual([
			'2026-08-17T09:30:00+08:00',
			'2026-08-17T15:00:00+08:00',
		]);
		expect(result.every((item) => item.previous_close === 67.36)).toBe(true);
	});

	it('keeps an API-supplied previous close', () => {
		const result = latestTradingDayKLines([
			line('2026-08-17T09:30:00+08:00', 80.83, 67.36),
			line('2026-08-17T15:00:00+08:00', 80.83, 67.36),
		]);

		expect(result).toHaveLength(2);
		expect(result[0].previous_close).toBe(67.36);
	});
});
