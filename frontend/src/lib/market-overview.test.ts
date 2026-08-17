import { describe, expect, it } from 'vitest';
import { buildMarketBillboardPrompt, buildMarketModulePrompt, buildMarketPulsePrompt, marketOverviewGroups, resolveMarketOverviewView } from './market-overview';

describe('market overview navigation', () => {
	it('uses the approved product names and grouping', () => {
		expect(marketOverviewGroups.map((group) => group.name)).toEqual(['市场总览', '资金结构', '研究信号']);
		expect(marketOverviewGroups.flatMap((group) => group.modules.map((module) => module.name))).toContain('市场核心指数');
		expect(marketOverviewGroups.flatMap((group) => group.modules.map((module) => module.name))).toContain('融资融券余额');
		expect(marketOverviewGroups.flatMap((group) => group.modules.map((module) => module.name))).toContain('龙虎榜');
		expect(marketOverviewGroups.flatMap((group) => group.modules.map((module) => module.name))).not.toContain('全球温度');
		expect(marketOverviewGroups.flatMap((group) => group.modules.every((module) => module.status === 'ready'))).toEqual([true, true, true]);
	});

	it('resolves supported market hashes and falls back to pulse', () => {
		expect(resolveMarketOverviewView('#market/core-indexes')).toBe('core-indexes');
		expect(resolveMarketOverviewView('#market/billboard')).toBe('billboard');
		expect(resolveMarketOverviewView('#market/margin-balance')).toBe('margin-balance');
		expect(resolveMarketOverviewView('#market/global')).toBe('pulse');
		expect(resolveMarketOverviewView('#market/unknown')).toBe('pulse');
	});
});

describe('market module AI prompt', () => {
	it('includes margin balance trend evidence', () => {
		const prompt = buildMarketModulePrompt('margin-balance', {
			margins: [{
				trade_date: '2026-08-11', financing_balance: 2_700_000_000_000,
				securities_lending_balance: 30_000_000_000, margin_balance: 2_730_000_000_000,
				margin_balance_change: 12_000_000_000, financing_buy_amount: 120_000_000_000,
				financing_repay_amount: 108_000_000_000, financing_net_buy_amount: 12_000_000_000,
				securities_lending_sell_volume: 1_000_000, securities_lending_repay_volume: 800_000,
				meta: { source: 'eastmoney:margin-balance', fetched_at: '2026-08-11T18:00:00+08:00', latency_ms: 10, stale: false },
			}],
			meta: { source: 'eastmoney:margin-balance', fetched_at: '2026-08-11T18:00:00+08:00', latency_ms: 10, stale: false },
		}, '2026-08-11 18:10');
		expect(prompt).toContain('两融余额 2.73万亿');
		expect(prompt).toContain('融资净买入 120.00亿');
		expect(prompt).toContain('eastmoney:margin-balance');
	});

	it('keeps fund-flow values, source and fallback evidence', () => {
		const prompt = buildMarketModulePrompt('stock-flow', {
			flows: [{
				dimension: 'stock', code: '600001', symbol: '600001.SH', name: '样本股份', price: 12.3,
				change_percent: 2.5, main_net_inflow: 120_000_000, main_net_inflow_ratio: 8.2,
				inflow: 230_000_000, outflow: 110_000_000, net_inflow: 120_000_000, net_inflow_ratio: 4.2,
				main_inflow: 160_000_000, main_outflow: 40_000_000,
				retail_inflow: 70_000_000, retail_outflow: 70_000_000, retail_net_inflow: 0, retail_net_inflow_ratio: 0,
				super_large_net_inflow: 80_000_000, super_large_net_inflow_ratio: 5.1,
				large_net_inflow: 40_000_000, large_net_inflow_ratio: 3.1,
				medium_net_inflow: -10_000_000, medium_net_inflow_ratio: -0.6,
				small_net_inflow: -110_000_000, small_net_inflow_ratio: -7.6,
				leader_price: 0, leader_change_percent: 0, leader_net_inflow_ratio: 0,
				meta: { source: 'eastmoney:fund-flow', fetched_at: '2026-08-11T14:30:00+08:00', latency_ms: 10, stale: false },
			}],
			meta: { source: 'eastmoney:fund-flow', fetched_at: '2026-08-11T14:30:00+08:00', latency_ms: 10, stale: true, fallback_reason: '使用最近快照' },
		}, '2026-08-11 15:00');
		expect(prompt).toContain('样本股份（600001.SH）');
		expect(prompt).toContain('1.20亿');
		expect(prompt).toContain('eastmoney:fund-flow');
		expect(prompt).toContain('使用最近快照');
		expect(prompt).toContain('不得补造');
	});
});

describe('market billboard AI prompt', () => {
	it('includes seats, institution net flow, concentration, themes, streaks and two three-stock lists', () => {
		const item = {
			trade_date: '2026-08-11', symbol: '600001.SH', name: '样本股份', close_price: 12.3,
			change_percent: 10.01, turnover_rate: 18.2, reason: '日涨幅偏离值达7%',
			buy_amount: 100_000_000, sell_amount: 40_000_000, net_amount: 60_000_000,
			institution_buyers: 1, buy_seats: 5, sell_seats: 5,
			meta: { source: 'eastmoney:billboard', fetched_at: '2026-08-11T18:00:00+08:00', latency_ms: 10, stale: false },
		};
		const prompt = buildMarketBillboardPrompt({
			items: [item],
			details: {
				'2026-08-11|600001.SH|日涨幅偏离值达7%': {
					trade_date: '2026-08-11', symbol: '600001.SH', reason: item.reason,
					buy_seats: [{ direction: 'buy', rank: 1, name: '机构专用', buy_amount: 30_000_000, buy_ratio: 30, sell_amount: 2_000_000, sell_ratio: 5, net_amount: 28_000_000, institution: true }],
					sell_seats: [{ direction: 'sell', rank: 1, name: '样本营业部', buy_amount: 1_000_000, buy_ratio: 1, sell_amount: 12_000_000, sell_ratio: 30, net_amount: -11_000_000, institution: false }],
					meta: { source: 'eastmoney:billboard-seats', fetched_at: '2026-08-11T18:00:00+08:00', latency_ms: 10, stale: false },
				},
			},
			limitUp: {
				session_status: 'closed',
				current: { trade_date: '2026-08-11', limit_up_count: 30, board_count: 8, first_board_count: 22, max_streak: 5, reopened_count: 2, st_count: 0, total_amount: 0, levels: [{ level: 3, label: '3板', count: 1, stocks: [{ symbol: '600001.SH', name: '样本股份', price: 12.3, change_percent: 10.01, amount: 500_000_000, float_market_cap: 0, turnover_rate: 18.2, streak: 3, open_count: 0, industry: '软件', days: 3, count: 3, is_st: false, limit_regime: '10cm', raw_concepts: ['AI应用'], primary_theme: 'AI应用', secondary_themes: ['算力'], theme_confidence: 0.9, theme_evidence: [], theme_leader_role: '龙头' }] }], },
				previous: { trade_date: '2026-08-10', limit_up_count: 0, board_count: 0, first_board_count: 0, max_streak: 0, reopened_count: 0, st_count: 0, total_amount: 0, levels: [] },
				advance: [], industry_heat: [], concept_heat: [{ name: 'AI应用', count: 5, board_count: 2, max_streak: 3, previous_count: 3, heat: 88, leaders: ['样本股份'] }],
				meta: { source: 'limit-up:test', fetched_at: '2026-08-11T18:00:00+08:00', latency_ms: 10, stale: false },
			},
			meta: item.meta,
		}, '2026-08-11 18:10');

		expect(prompt).toContain('机构席位净额 2800.0万');
		expect(prompt).toContain('买方买一/前三集中度 30.00% / 30.00%');
		expect(prompt).toContain('连板高度 3板');
		expect(prompt).toContain('题材 AI应用、算力');
		expect(prompt).toContain('明日最值得观察的3个名单');
		expect(prompt).toContain('明日需要优先止损/割肉评估的3个名单');
		expect(prompt).toContain('身份未确认');
	});
});

describe('market pulse AI prompt', () => {
	it('includes timestamps, sources, news and theme evidence', () => {
		const prompt = buildMarketPulsePrompt([
			{
				title: '测试快讯',
				published_at: '2026-08-11T14:30:00+08:00',
				meta: { source: 'cls', fetched_at: '2026-08-11T14:30:01+08:00', latency_ms: 10, stale: false },
			},
		], [
			{
				theme: 'semiconductor',
				name: '半导体',
				change_percent: 2.3,
				main_net_inflow: 120_000_000,
				rising_nodes: 8,
				falling_nodes: 2,
				matched_nodes: 10,
				total_nodes: 10,
				top_node_change_percent: 3.1,
				leaders: ['示例股份'],
			},
		], '2026-08-11 15:00');

		expect(prompt).toContain('测试快讯');
		expect(prompt).toContain('来源：cls');
		expect(prompt).toContain('半导体');
		expect(prompt).toContain('严格区分事实与推断');
	});
});
