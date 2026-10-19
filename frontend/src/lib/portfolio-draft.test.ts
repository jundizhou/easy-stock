import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { activePortfolioPlan, addPortfolioPlan, portfolioDraftStorageKey, portfolioDraftToHoldings, portfolioDraftToHoldingsWithAssets, portfolioPlansStorageKey, readPortfolioDraft, readPortfolioPlans, removePortfolioPlan, renamePortfolioPlan, selectPortfolioPlan, writePortfolioDraft, type PortfolioDraft } from './portfolio-draft';

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
	it('uses the latest quote for automatic weight when quantity is provided', () => {
		const result = portfolioDraftToHoldingsWithAssets([{ symbol: '688002.SH', name: '睿创微纳', weight: 1, quantity: '10000', costPrice: '40' }], 500000, { '688002.SH': { symbol: '688002.SH', price: 45.02 } as never });
		expect(result[0].weight_percent).toBe(90);
	});
	it('submits the whole percentage the inspection API stores', () => {
		const holdings = portfolioDraftToHoldingsWithAssets([{ symbol: '600206.SH', name: '有研新材', weight: 10, quantity: '900', costPrice: '46.95' }], 500000, { '600206.SH': { symbol: '600206.SH', price: 37.51 } as never });
		expect(holdings[0].weight_percent).toBe(7);
		expect(Number.isInteger(holdings[0].weight_percent)).toBe(true);
	});
	it('keeps the apportioned total within 100 when every position rounds up', () => {
		const quotes = Object.fromEntries(Array.from({ length: 6 }, (_, index) => [`60000${index}.SH`, { symbol: `60000${index}.SH`, price: 10 }]));
		const draft = Array.from({ length: 6 }, (_, index) => ({ symbol: `60000${index}.SH`, name: `样本${index}`, weight: 10, quantity: '1660', costPrice: '10' }));
		const weights = portfolioDraftToHoldingsWithAssets(draft, 100000, quotes as never).map((item) => item.weight_percent);
		expect(weights).toEqual([17, 17, 17, 17, 16, 16]);
		expect(weights.reduce((sum, value) => sum + value, 0)).toBe(100);
	});
	it('never rounds a non-empty position down to the 0% the API rejects', () => {
		const quotes = { '600001.SH': { symbol: '600001.SH', price: 1 }, '600002.SH': { symbol: '600002.SH', price: 1 } } as never;
		const weights = portfolioDraftToHoldingsWithAssets([
			{ symbol: '600001.SH', name: '零碎仓位', weight: 10, quantity: '1', costPrice: '1' },
			{ symbol: '600002.SH', name: '主仓', weight: 10, quantity: '9999', costPrice: '1' },
		], 10000, quotes).map((item) => item.weight_percent);
		expect(weights).toEqual([1, 99]);
	});
	it('takes the 1% floor overrun back from the largest position', () => {
		const quotes = Object.fromEntries(Array.from({ length: 10 }, (_, index) => [`60010${index}.SH`, { symbol: `60010${index}.SH`, price: 1 }]));
		const draft = Array.from({ length: 10 }, (_, index) => ({ symbol: `60010${index}.SH`, name: `样本${index}`, weight: 10, quantity: index === 9 ? '96000' : '1', costPrice: '1' }));
		const weights = portfolioDraftToHoldingsWithAssets(draft, 100000, quotes as never).map((item) => item.weight_percent);
		expect(weights).toEqual([1, 1, 1, 1, 1, 1, 1, 1, 1, 87]);
	});
	it('falls back to the stored share when no share count is provided', () => {
		const weights = portfolioDraftToHoldingsWithAssets([
			{ symbol: '600519.SH', name: '贵州茅台', weight: 35.6, costPrice: '1420.5' },
			{ symbol: '000858.SZ', name: '五粮液', weight: 20, costPrice: '' },
		], 500000).map((item) => item.weight_percent);
		expect(weights).toEqual([36, 20]);
	});
});

// Use real storage round-trips to catch migration and cross-plan data loss.
describe('saved portfolio plans', () => {
	const legacy = { profile: 'steady', horizon: 'medium', researchLevel: 'deep', holdings: [{ symbol: '600519.SH', name: '贵州茅台', weight: 35, costPrice: '1420.5' }] };
	let values: Map<string, string>;
	let storage: { getItem: ReturnType<typeof vi.fn>; setItem: ReturnType<typeof vi.fn> };
	let events: ReturnType<typeof vi.fn>;
	beforeEach(() => {
		values = new Map();
		storage = { getItem: vi.fn((key: string) => values.get(key) ?? null), setItem: vi.fn((key: string, value: string) => values.set(key, value)) };
		events = vi.fn();
		vi.stubGlobal('window', { localStorage: storage, dispatchEvent: events });
	});
	afterEach(() => vi.unstubAllGlobals());

	it('preserves the original holdings and settings while adding a separate empty plan', () => {
		values.set(portfolioDraftStorageKey, JSON.stringify(legacy));
		expect(readPortfolioDraft()).toEqual({ ...legacy, totalAssets: 500000 });
		const added = addPortfolioPlan();
		expect(added.plans[0]).toMatchObject({ name: '方案 1', draft: legacy });
		expect(activePortfolioPlan(added)).toMatchObject({ name: '方案 2', draft: { profile: 'balanced', horizon: 'swing', researchLevel: 'standard', holdings: [] } });
		writePortfolioDraft({ profile: 'aggressive', horizon: 'short', researchLevel: 'standard', holdings: [{ symbol: '000858.SZ', name: '五粮液', weight: 20, costPrice: '100' }] });
		const second = readPortfolioDraft();
		selectPortfolioPlan(added.plans[0].id);
		expect(readPortfolioDraft()).toEqual({ ...legacy, totalAssets: 500000 });
		selectPortfolioPlan(added.activeId);
		expect(readPortfolioDraft()).toEqual(second);
		expect(readPortfolioPlans().activeId).toBe(added.activeId);
		expect(JSON.parse(values.get(portfolioPlansStorageKey)!)).toEqual(readPortfolioPlans());
		expect(JSON.parse(values.get(portfolioDraftStorageKey)!)).toEqual(legacy);
	});

	it('writes a dialog snapshot back to its original plan after another plan is selected', () => {
		const original = readPortfolioPlans().activeId;
		const second = addPortfolioPlan().activeId;
		writePortfolioDraft(legacy as PortfolioDraft, original);
		expect(readPortfolioPlans().activeId).toBe(second);
		expect(readPortfolioDraft().holdings).toEqual([]);
		selectPortfolioPlan(original);
		expect(readPortfolioDraft()).toEqual({ ...legacy, totalAssets: 500000 });
	});

	it('persists names and removes only the chosen plan, keeping at least one', () => {
		writePortfolioDraft(legacy as PortfolioDraft);
		const first = readPortfolioPlans().activeId;
		const second = addPortfolioPlan().activeId;
		renamePortfolioPlan(second, '  波段组合  ');
		expect(activePortfolioPlan(readPortfolioPlans()).name).toBe('波段组合');
		expect(() => renamePortfolioPlan(second, ' ')).toThrow('请输入方案名称');
		removePortfolioPlan(second);
		expect(readPortfolioPlans().activeId).toBe(first);
		expect(readPortfolioDraft()).toEqual({ ...legacy, totalAssets: 500000 });
		expect(() => writePortfolioDraft(legacy as PortfolioDraft, second)).toThrow('已被删除');
		expect(() => removePortfolioPlan(first)).toThrow('至少保留一份');
	});

	it('does not switch the active plan when deleting another one', () => {
		const first = readPortfolioPlans().activeId;
		const second = addPortfolioPlan().activeId;
		removePortfolioPlan(first);
		expect(readPortfolioPlans().activeId).toBe(second);
	});

	it('recovers the legacy draft from malformed storage and repairs a missing selection', () => {
		values.set(portfolioDraftStorageKey, JSON.stringify(legacy));
		values.set(portfolioPlansStorageKey, '{broken');
		expect(readPortfolioDraft()).toEqual({ ...legacy, totalAssets: 500000 });
		values.set(portfolioPlansStorageKey, JSON.stringify({ activeId: 'missing', plans: [null, { id: 'valid', name: '已保存', draft: legacy }, { id: 'valid', draft: null }] }));
		expect(readPortfolioPlans()).toEqual({ activeId: 'valid', plans: [{ id: 'valid', name: '已保存', draft: { ...legacy, totalAssets: 500000 } }] });
	});

	it('persists independent asset amounts and the original price when costs are cleared', () => {
		const first = readPortfolioPlans().activeId;
		const draft: PortfolioDraft = { profile: 'balanced', totalAssets: 800000, holdings: [{ symbol: '600001.SH', name: '示例', weight: 30, costPrice: '', entryPrice: 20, entryPriceTime: '2026-10-10T07:00:00Z' }] };
		writePortfolioDraft(draft);
		const second = addPortfolioPlan().activeId;
		expect(readPortfolioDraft().totalAssets).toBe(500000);
		selectPortfolioPlan(first);
		expect(readPortfolioDraft()).toMatchObject(draft);
		expect(portfolioDraftToHoldings(readPortfolioDraft().holdings)[0].cost_price).toBe(20);
		selectPortfolioPlan(second);
		expect(readPortfolioDraft().holdings).toEqual([]);
	});

	it('reports failed persistence without changing saved plans or notifying consumers', () => {
		writePortfolioDraft(legacy as PortfolioDraft);
		const saved = values.get(portfolioPlansStorageKey);
		events.mockClear();
		storage.setItem.mockImplementation(() => { throw new Error('QuotaExceededError'); });
		expect(() => addPortfolioPlan()).toThrow('QuotaExceededError');
		expect(values.get(portfolioPlansStorageKey)).toBe(saved);
		expect(events).not.toHaveBeenCalled();
	});
});
