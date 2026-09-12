import { describe, expect, it } from 'vitest';
import type { StockDirectoryEntry } from './backend';
import { resolveWatchlistInput } from './watchlist';

const directory: StockDirectoryEntry[] = [
	{ symbol: '600519.SH', code: '600519', name: '贵州茅台' },
	{ symbol: '000001.SZ', code: '000001', name: '平安银行' },
	{ symbol: '601398.SH', code: '601398', name: '工商银行' },
	{ symbol: '601939.SH', code: '601939', name: '建设银行' },
];

describe('resolveWatchlistInput', () => {
	it('passes bare and prefixed codes through for backend normalization', () => {
		expect(resolveWatchlistInput('600519', directory)).toEqual({ symbol: '600519' });
		expect(resolveWatchlistInput(' 000001 ', directory)).toEqual({ symbol: '000001' });
		expect(resolveWatchlistInput('sh600519', directory)).toEqual({ symbol: 'SH600519' });
		expect(resolveWatchlistInput('600519.SH', directory)).toEqual({ symbol: '600519.SH' });
	});

	it('resolves an exact Chinese name to its symbol', () => {
		expect(resolveWatchlistInput('贵州茅台', directory)).toEqual({ symbol: '600519.SH' });
	});

	it('resolves a unique partial name to its symbol', () => {
		expect(resolveWatchlistInput('茅台', directory)).toEqual({ symbol: '600519.SH' });
	});

	it('asks for a code instead of guessing when a name is ambiguous', () => {
		const resolution = resolveWatchlistInput('银行', directory);
		expect(resolution.symbol).toBeUndefined();
		expect(resolution.notice).toContain('匹配到 3 只');
		expect(resolution.notice).toContain('平安银行');
	});

	it('returns the raw input when the directory has no match so the backend can reject it', () => {
		expect(resolveWatchlistInput('不存在的名字', directory)).toEqual({ symbol: '不存在的名字' });
	});

	it('ignores empty input', () => {
		expect(resolveWatchlistInput('   ', directory)).toEqual({});
	});
});
