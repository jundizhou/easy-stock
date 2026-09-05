import { describe, expect, it } from 'vitest';
import { isTomorrowOutlookDegraded, visibleUSMarketSummary } from './ReviewDiary';

describe('visibleUSMarketSummary', () => {
	it('removes internal data-source diagnostics and duplicate impact analysis', () => {
		expect(visibleUSMarketSummary('指数：纳斯达克 -1.03%；数据质量：已切换腾讯行情；影响解读：科技偏弱')).toBe('指数：纳斯达克 -1.03%');
	});

	it('removes a legacy impact analysis suffix without data-quality text', () => {
		expect(visibleUSMarketSummary('指数：标普500 -0.71%；影响解读：外盘承压')).toBe('指数：标普500 -0.71%');
	});

	it('keeps a clean factual summary unchanged', () => {
		expect(visibleUSMarketSummary('指数：道琼斯 -0.79%；数据时间：2026-09-02 04:00')).toBe('指数：道琼斯 -0.79%；数据时间：2026-09-02 04:00');
	});
});

describe('isTomorrowOutlookDegraded', () => {
	it('uses the dedicated status from newly generated summaries', () => {
		expect(isTomorrowOutlookDegraded({ tomorrow_outlook_degraded: true })).toBe(true);
		expect(isTomorrowOutlookDegraded({ tomorrow_outlook_degraded: false })).toBe(false);
	});

	it('recognizes legacy summaries from their phase error', () => {
		expect(isTomorrowOutlookDegraded({ generation_errors: ['明日计划归纳：上游模型超时'] })).toBe(true);
		expect(isTomorrowOutlookDegraded({ generation_errors: ['市场结构归纳：上游模型超时'] })).toBe(false);
	});
});
