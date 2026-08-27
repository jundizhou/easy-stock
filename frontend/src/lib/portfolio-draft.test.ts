import { describe, expect, it } from 'vitest';
import { portfolioDraftToHoldings } from './portfolio-draft';

describe('portfolio draft mapping', () => {
	it('keeps the inspection and tomorrow expectation request shape identical', () => {
		expect(portfolioDraftToHoldings([
			{ symbol: '600519.SH', name: '贵州茅台', weight: 35, costPrice: '1420.5' },
			{ symbol: '000858.SZ', name: '五粮液', weight: 20, costPrice: '' },
		])).toEqual([
			{ symbol: '600519.SH', name: '贵州茅台', weight_percent: 35, cost_price: 1420.5 },
			{ symbol: '000858.SZ', name: '五粮液', weight_percent: 20 },
		]);
	});
});
