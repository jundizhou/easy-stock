import { describe, expect, it } from 'vitest';
import { KLine } from './backend';
import { aggregateThemeIndex } from './theme-index';
const bar = (date: string, close: number, open = close): KLine => ({ symbol: 'theme', time: `${date}T00:00:00+08:00`, open, close, high: Math.max(open, close) + 1, low: Math.min(open, close) - 1, volume: 10, amount: 100, meta: { source: 'test', fetched_at: '', latency_ms: 0, stale: false } });
describe('theme index aggregation', () => {
	it('preserves OHLC and sums turnover across holidays without inventing sessions', () => {
		const lines = [bar('2026-09-30', 100), bar('2026-10-08', 102, 101), bar('2026-10-09', 104, 102)];
		const result = aggregateThemeIndex(lines, 'week');
		expect(result).toHaveLength(2);
		expect(result[1]).toMatchObject({ open: 101, close: 104, high: 105, low: 100, volume: 20, amount: 200, previous_close: 100, change_percent: 4.0000000000000036 });
		expect(lines[1].close).toBe(102);
	});
	it('uses Monday weeks across year boundaries', () => {
		expect(aggregateThemeIndex([bar('2025-12-31', 100), bar('2026-01-02', 101), bar('2026-01-05', 102)], 'week')).toHaveLength(2);
	});
	it('uses exchange dates rather than the browser timezone', () => {
		const line = { ...bar('2026-09-30', 100), time: '2026-09-30T16:00:00Z' };
		const next = bar('2026-10-02', 101);
		expect(aggregateThemeIndex([line, next], 'month')).toHaveLength(1);
	});
	it('sorts daily bars without mutating the server cache', () => {
		const lines = [bar('2026-09-30', 100), bar('2026-09-29', 99)];
		expect(aggregateThemeIndex(lines, 'day')[0].close).toBe(99);
		expect(lines[0].close).toBe(100);
	});
});
