import { describe, expect, it } from 'vitest';
import { buildMarketModulePrompt, buildMarketPulsePrompt, marketOverviewGroups, resolveMarketOverviewView } from './market-overview';

describe('market overview navigation', () => {
	it('uses the approved product names and grouping', () => {
		expect(marketOverviewGroups.map((group) => group.name)).toEqual(['市场总览', '资金结构', '研究信号']);
		expect(marketOverviewGroups.flatMap((group) => group.modules.map((module) => module.name))).toContain('市场核心指数');
		expect(marketOverviewGroups.flatMap((group) => group.modules.map((module) => module.name))).toContain('龙虎榜');
		expect(marketOverviewGroups.flatMap((group) => group.modules.map((module) => module.name))).not.toContain('全球温度');
		expect(marketOverviewGroups.flatMap((group) => group.modules.every((module) => module.status === 'ready'))).toEqual([true, true, true]);
	});

	it('resolves supported market hashes and falls back to pulse', () => {
		expect(resolveMarketOverviewView('#market/core-indexes')).toBe('core-indexes');
		expect(resolveMarketOverviewView('#market/billboard')).toBe('billboard');
		expect(resolveMarketOverviewView('#market/global')).toBe('pulse');
		expect(resolveMarketOverviewView('#market/unknown')).toBe('pulse');
	});
});

describe('market module AI prompt', () => {
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
