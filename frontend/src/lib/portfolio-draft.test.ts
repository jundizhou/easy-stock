import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { activePortfolioPlan, addPortfolioPlan, portfolioDraftStorageKey, portfolioDraftToHoldings, portfolioPlansStorageKey, readPortfolioDraft, readPortfolioPlans, removePortfolioPlan, renamePortfolioPlan, selectPortfolioPlan, writePortfolioDraft, type PortfolioDraft } from './portfolio-draft';

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
