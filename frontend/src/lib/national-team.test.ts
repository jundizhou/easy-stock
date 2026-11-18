import { describe, expect, it } from 'vitest';
import { etfMoney } from './national-team';
import { resolveMarketOverviewView } from './market-overview';

describe('national team ETF evidence', () => {
	it('keeps real zero distinct from unavailable amounts and uses CNY units', () => {
		expect(etfMoney(null)).toBe('--'); expect(etfMoney(0)).toBe('0.00 亿'); expect(etfMoney(-2e8)).toBe('−2.00 亿');
		expect(resolveMarketOverviewView('#market/national-team')).toBe('national-team');
	});

});
