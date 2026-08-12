import { describe, expect, it } from 'vitest';
import { compareSortValues, nextSortState } from './MarketDataViews';

describe('market data column sorting', () => {
	it('toggles the active column and applies the column default direction', () => {
		expect(nextSortState({ key: 'score', direction: 'desc' }, 'score', 'desc')).toEqual({ key: 'score', direction: 'asc' });
		expect(nextSortState({ key: 'score', direction: 'desc' }, 'name', 'asc')).toEqual({ key: 'name', direction: 'asc' });
	});

	it('sorts numbers and Chinese labels while keeping missing values at the bottom', () => {
		expect(compareSortValues(12, 5, 'desc')).toBeLessThan(0);
		expect(compareSortValues('行业乙', '行业甲', 'asc')).toBeGreaterThan(0);
		expect(compareSortValues(undefined, 5, 'asc')).toBeGreaterThan(0);
	});
});
