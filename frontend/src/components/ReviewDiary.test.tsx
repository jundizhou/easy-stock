import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { DailyValidationEmpty, isTomorrowOutlookDegraded, visibleUSMarketSummary } from './ReviewDiary';

describe('daily validation empty states', () => {
	it('keeps the summary accessible while waiting for Monday close', () => {
		const html = renderToStaticMarkup(<DailyValidationEmpty running={false} hasSummary job={{ summary_date: '2026-10-23', verification_date: '2026-10-26', status: 'waiting', stage: 'waiting', message: '核验日 2026-10-26（周一）15:00 收盘后可开始验证', result_available: false, can_start: false }} onValidate={() => {}} onOpenSummary={() => {}} />);
		expect(html).toContain('2026-10-26');
		expect(html).toContain('等待核验日收盘');
		expect(html).toContain('disabled=""');
		expect(html).toContain('查看 AI 总结');
		expect(html).not.toContain('先生成一份昨日 AI 总结');
	});
	it('always provides an exit when a summary is unavailable', () => {
		const html = renderToStaticMarkup(<DailyValidationEmpty running={false} hasSummary={false} job={null} onValidate={() => {}} onOpenSummary={() => {}} />);
		expect(html).toContain('查看 AI 总结');
		expect(html).not.toContain('已完成');
	});
});

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
