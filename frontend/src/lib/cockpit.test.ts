import { describe, expect, it } from 'vitest';
import type { MarketEmotionHistory, MarketIndexSnapshot, PortfolioInspectionJob, SourceHealth } from './backend';
import { buildAttentionItems, buildCockpitBriefPrompt, emotionPhaseTone, emotionTrend, formatPercent, latestInspection } from './cockpit';

const meta = { source: 'test', fetched_at: '2026-10-20T02:00:00Z', latency_ms: 1, stale: false };

const index = (name: string, change: number): MarketIndexSnapshot => ({
	id: name, secid: name, code: name, name, region: 'CN', market: 'SH', currency: 'CNY', price: 3000, change: 0, change_percent: change, status: 'trading', meta,
});

const emotion = (overrides: Partial<NonNullable<MarketEmotionHistory['latest']>> = {}, raw: Partial<NonNullable<MarketEmotionHistory['latest']>['raw']> = {}): MarketEmotionHistory => ({
	points: [],
	latest: {
		model_version: 1, trade_date: '2026-10-20', emotion_score: 42, phase: '混沌/过渡', confidence: 'high', history_samples: 30,
		raw: { limit_up_count: 60, limit_down_count: 2, broken_count: 10, first_board_count: 40, board_count: 20, max_streak: 5, reopened_count: 3, final_break_rate: 0.2, reopen_success_rate: 0.4, previous_limit_up_return: 0, previous_board_return: 0, open_premium: 0, core_return: 0, advance_rate: 0, theme_focus: 0, leader_gap: 0, ladder_continuity: 0, high_sample_count: 0, high_weak_count: 0, high_kill: 0, high_limit_down: 0, high_average_return: 0, high_down_rate: 0, high_advance_rate: 0, height_collapse: 0, high_risk_score: 0, mid_kill: 0, low_kill: 0, quote_coverage: 1, limit_up_10cm: 0, limit_up_20cm: 0, limit_up_30cm: 0, max_streak_10cm: 0, max_streak_20cm: 0, max_streak_30cm: 0, ...raw },
		scores: { heat: 0, profit: 0, structure: 0, total: 0 }, source: 'test', updated_at: '',
		...overrides,
	},
	cache: { mode: 'test', cached_days: 0, bootstrap_days: 0 },
});

const inspection = (overrides: Partial<PortfolioInspectionJob> = {}): PortfolioInspectionJob => ({
	id: 'job-1', status: 'succeeded', stage: 'done', request: {} as PortfolioInspectionJob['request'], results: [], completed_stocks: 1, total_stocks: 1, coverage_percent: 100, current_symbols: [], message: '', report_available: true, started_at: '2026-10-20T01:00:00Z', ...overrides,
});

const source = (id: string, ok: boolean): SourceHealth => ({ id, name: `源${id}`, category: 'quote', ok, checked_at: '' });

describe('cockpit attention rules', () => {
	it('reports the all-clear state only when modules loaded without alerts', () => {
		const items = buildAttentionItems({ indexes: [index('上证指数', 0.3)], sources: [source('a', true)] });
		expect(items).toEqual([expect.objectContaining({ level: 'info', title: '暂无需要处理的事项', target: null })]);
	});

	it('returns no items while nothing has loaded yet', () => {
		expect(buildAttentionItems({})).toEqual([]);
	});

	it('flags index drops at the -1% threshold and routes to the market page', () => {
		const items = buildAttentionItems({ indexes: [index('创业板指', -1.2), index('上证指数', -0.9)] });
		expect(items).toHaveLength(1);
		expect(items[0]).toMatchObject({ level: 'warn', title: '创业板指 下跌 1.20%', target: 'market' });
	});

	it('flags retreat and freezing emotion phases', () => {
		expect(buildAttentionItems({ emotion: emotion({ phase: '退潮' }) })[0]).toMatchObject({ title: '短线情绪处于「退潮」', target: 'limit-up' });
		expect(buildAttentionItems({ emotion: emotion({ phase: '冰点' }) })[0]?.title).toContain('冰点');
		expect(buildAttentionItems({ emotion: emotion({ phase: '发酵/主升' }) })).toEqual([expect.objectContaining({ level: 'info' })]);
	});

	it('flags limit-down counts only when they outnumber limit-up counts and are material', () => {
		expect(buildAttentionItems({ emotion: emotion({}, { limit_up_count: 10, limit_down_count: 12 }) }).map((item) => item.id)).toContain('limit-down-heavy');
		expect(buildAttentionItems({ emotion: emotion({}, { limit_up_count: 10, limit_down_count: 3 }) }).map((item) => item.id)).not.toContain('limit-down-heavy');
	});

	it('uses the newest inspection and reports high risk as danger first', () => {
		const older = inspection({ id: 'old', started_at: '2026-10-18T01:00:00Z', report: { conclusion: { risk_level: '低', health_score: 90 } } as NonNullable<PortfolioInspectionJob['report']> });
		const newest = inspection({ id: 'new', started_at: '2026-10-20T01:00:00Z', report: { conclusion: { risk_level: '高', health_score: 40, primary_risks: ['单一题材集中'], executive_summary: '' } } as unknown as NonNullable<PortfolioInspectionJob['report']> });
		expect(latestInspection([older, newest])?.id).toBe('new');
		const items = buildAttentionItems({ inspections: [older, newest], indexes: [index('上证指数', -2)] });
		expect(items[0]).toMatchObject({ level: 'danger', title: '持仓风险等级：高', detail: '单一题材集中', target: 'portfolio-inspection' });
	});

	it('suggests resume for an interrupted inspection', () => {
		const items = buildAttentionItems({ inspections: [inspection({ status: 'interrupted', resume_available: true })] });
		expect(items[0]).toMatchObject({ id: 'inspection-incomplete', level: 'warn', target: 'portfolio-inspection' });
		expect(items[0].detail).toContain('恢复');
	});

	it('summarises failed data sources and sorts danger before warn before info', () => {
		const items = buildAttentionItems({ sources: [source('a', false), source('b', true), source('c', false)], indexes: [index('上证指数', -1.5)] });
		expect(items.map((item) => item.id)).toEqual(['index-上证指数', 'sources-failed']);
		expect(items[1]).toMatchObject({ title: '2 个数据源异常', detail: '源a、源c', target: 'themes' });
	});
});

describe('cockpit helpers', () => {
	it('formats percents and maps phases to tones', () => {
		expect(formatPercent(1.234)).toBe('+1.23%');
		expect(formatPercent(-0.5)).toBe('-0.50%');
		expect(formatPercent(undefined)).toBe('--');
		expect(emotionPhaseTone('退潮')).toBe('down');
		expect(emotionPhaseTone('高潮')).toBe('up');
		expect(emotionPhaseTone('混沌/过渡')).toBe('neutral');
	});

	it('keeps only the most recent points for the sparkline', () => {
		const points = Array.from({ length: 20 }, (_, i) => ({ emotion_score: i }) as MarketEmotionHistory['points'][number]);
		expect(emotionTrend(points, 12)).toHaveLength(12);
		expect(emotionTrend(points, 12)[11]).toBe(19);
		expect(emotionTrend(undefined)).toEqual([]);
	});

	it('builds a brief prompt that carries only loaded data and the no-advice constraint', () => {
		const input = { indexes: [index('上证指数', -1.5)], emotion: emotion({ phase: '退潮' }), sources: [source('a', false)] };
		const prompt = buildCockpitBriefPrompt(input, buildAttentionItems(input));
		expect(prompt).toContain('上证指数 -1.50%');
		expect(prompt).toContain('「退潮」');
		expect(prompt).toContain('源a');
		expect(prompt).toContain('不构成投资建议');
		expect(prompt).toContain('不要编造');
	});
});
