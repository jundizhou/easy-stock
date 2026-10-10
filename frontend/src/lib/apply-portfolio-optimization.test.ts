import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Quote } from './backend';
import { applyPortfolioOptimization } from './apply-portfolio-optimization';
import { loadPortfolioQuotes } from './use-portfolio-quotes';
import { addPortfolioPlan, portfolioPlansStorageKey, readPortfolioPlans, removePortfolioPlan, renamePortfolioPlan, writePortfolioDraft } from './portfolio-draft';

vi.mock('./use-portfolio-quotes', () => ({ loadPortfolioQuotes: vi.fn() }));
const config = { backendUrl: 'http://localhost', token: '' };
const target = [{ symbol: '600001.SH', name: '保留股', weight_percent: 30, cost_price: 8 }, { symbol: '600003.SH', name: '新股', weight_percent: 50 }];
const draft = { profile: 'steady' as const, totalAssets: 800000, horizon: 'medium' as const, researchLevel: 'deep' as const,
	holdings: [{ symbol: '600001.SH', name: '保留股', weight: 50, costPrice: '8' }, { symbol: '600002.SH', name: '卖出股', weight: 30, costPrice: '10' }] };
const quote = (symbol: string, price: number) => ({ symbol, price, trade_time: '2026-10-10T07:00:00Z', meta: { stale: false } } as Quote);

describe('applying optimization to the original saved plan', () => {
	let values: Map<string, string>;
	let setItem: ReturnType<typeof vi.fn>;
	let originalId: string;
	let otherId: string;
	beforeEach(() => {
		values = new Map();
		setItem = vi.fn((key: string, value: string) => values.set(key, value));
		vi.stubGlobal('window', { localStorage: { getItem: (key: string) => values.get(key), setItem }, dispatchEvent: vi.fn() });
		writePortfolioDraft(draft);
		originalId = readPortfolioPlans().activeId;
		otherId = addPortfolioPlan().activeId;
		vi.mocked(loadPortfolioQuotes).mockReset().mockResolvedValue({ '600001.SH': quote('600001.SH', 12.5), '600003.SH': quote('600003.SH', 25) });
	});
	afterEach(() => vi.unstubAllGlobals());

	it('writes all fresh costs to the bound plan even when another plan is active', async () => {
		const beforeTarget = JSON.stringify(target);
		const beforeOther = readPortfolioPlans().plans[1];
		const next = await applyPortfolioOptimization(config, originalId, target);
		expect(next.activeId).toBe(otherId);
		expect(next.plans[1]).toEqual(beforeOther);
		expect(next.plans[0].draft).toEqual({ ...draft, holdings: [
			{ symbol: '600001.SH', name: '保留股', weight: 30, costPrice: '12.5', entryPrice: 12.5, entryPriceTime: '2026-10-10T07:00:00Z' },
			{ symbol: '600003.SH', name: '新股', weight: 50, costPrice: '25', entryPrice: 25, entryPriceTime: '2026-10-10T07:00:00Z' },
		] });
		expect(JSON.stringify(target)).toBe(beforeTarget);
		expect(loadPortfolioQuotes).toHaveBeenCalledWith(config, ['600001.SH', '600003.SH'], undefined);
		expect(readPortfolioPlans()).toEqual(next);
	});

	it('keeps the entire original plan when any quote is missing or stale', async () => {
		const before = values.get(portfolioPlansStorageKey);
		for (const prices of [{ '600001.SH': quote('600001.SH', 12.5) }, { '600001.SH': quote('600001.SH', 12.5), '600003.SH': { ...quote('600003.SH', 25), meta: { stale: true } } as Quote }] as Record<string, Quote>[]) {
			vi.mocked(loadPortfolioQuotes).mockResolvedValueOnce(prices);
			await expect(applyPortfolioOptimization(config, originalId, target)).rejects.toThrow('原方案未修改');
			expect(values.get(portfolioPlansStorageKey)).toBe(before);
		}
	});

	it('rejects a missing/deleted original instead of falling back to the active plan', async () => {
		removePortfolioPlan(originalId);
		const before = values.get(portfolioPlansStorageKey);
		await expect(applyPortfolioOptimization(config, originalId, target)).rejects.toThrow('不存在或已删除');
		await expect(applyPortfolioOptimization(config, '', target)).rejects.toThrow('不存在或已删除');
		expect(loadPortfolioQuotes).not.toHaveBeenCalled();
		expect(values.get(portfolioPlansStorageKey)).toBe(before);
	});

	it('does not overwrite edits or resurrect a plan deleted while quotes are loading', async () => {
		vi.mocked(loadPortfolioQuotes).mockImplementationOnce(async () => { writePortfolioDraft({ ...draft, totalAssets: 900000 }, originalId); return { '600001.SH': quote('600001.SH', 12), '600003.SH': quote('600003.SH', 25) }; });
		await expect(applyPortfolioOptimization(config, originalId, target)).rejects.toThrow('发生修改');
		expect(readPortfolioPlans().plans[0].draft.totalAssets).toBe(900000);
		expect(readPortfolioPlans().plans[0].draft.holdings).toEqual(draft.holdings);
		vi.mocked(loadPortfolioQuotes).mockImplementationOnce(async () => { removePortfolioPlan(originalId); return { '600001.SH': quote('600001.SH', 12), '600003.SH': quote('600003.SH', 25) }; });
		await expect(applyPortfolioOptimization(config, originalId, target)).rejects.toThrow('已被删除');
		expect(readPortfolioPlans().plans).toHaveLength(1);
	});

	it('preserves a rename during quote loading and uses the stable ID', async () => {
		vi.mocked(loadPortfolioQuotes).mockImplementationOnce(async () => { renamePortfolioPlan(originalId, '新名字'); return { '600001.SH': quote('600001.SH', 12), '600003.SH': quote('600003.SH', 25) }; });
		const next = await applyPortfolioOptimization(config, originalId, target);
		expect(next.plans[0].name).toBe('新名字');
		expect(next.plans[0].draft.holdings[0].costPrice).toBe('12');
	});

	it('keeps the saved plan on network, cancellation, or storage failure', async () => {
		const before = values.get(portfolioPlansStorageKey);
		vi.mocked(loadPortfolioQuotes).mockRejectedValueOnce(new Error('行情失败'));
		await expect(applyPortfolioOptimization(config, originalId, target)).rejects.toThrow('行情失败');
		const controller = new AbortController(); controller.abort();
		await expect(applyPortfolioOptimization(config, originalId, target, controller.signal)).rejects.toThrow();
		setItem.mockImplementationOnce(() => { throw new Error('磁盘已满'); });
		await expect(applyPortfolioOptimization(config, originalId, target)).rejects.toThrow('磁盘已满');
		expect(values.get(portfolioPlansStorageKey)).toBe(before);
	});
});
