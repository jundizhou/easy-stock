import { describe, expect, it } from 'vitest';
import { KLine, SectorMap, ThemeOverview } from './backend';
import { buildThemeStocks, calculateThemeEmotion, rankThemeOverviews, themeStrengthScore } from './short-term';

const meta = { source: 'test', fetched_at: '', latency_ms: 0, stale: false };

describe('short-term theme helpers', () => {
	it('scores stronger and broader themes above weak themes', () => {
		const strong: ThemeOverview = {
			theme: 'strong', name: '强题材', change_percent: 3, main_net_inflow: 800_000_000,
			rising_nodes: 4, falling_nodes: 1, matched_nodes: 5, total_nodes: 5, top_node_change_percent: 6,
		};
		const weak: ThemeOverview = {
			theme: 'weak', name: '弱题材', change_percent: -2, main_net_inflow: -500_000_000,
			rising_nodes: 1, falling_nodes: 4, matched_nodes: 5, total_nodes: 5, top_node_change_percent: 0,
		};
		expect(calculateThemeEmotion(strong).score).toBeGreaterThan(calculateThemeEmotion(weak).score);
		expect(rankThemeOverviews([weak, strong])[0].theme).toBe('strong');
	});

	it('uses backend trend scores and stages for narrative themes', () => {
		const trend: ThemeOverview = {
			theme: 'trend-compute-leasing', name: '算力租赁', change_percent: 1.2, main_net_inflow: 0,
			rising_nodes: 4, falling_nodes: 1, matched_nodes: 5, total_nodes: 20,
			top_node_change_percent: 8.76, trend_score: 82, trend_stage: '主升',
		};
		const emotion = calculateThemeEmotion(trend);
		expect(emotion.score).toBe(82);
		expect(emotion.stage).toBe('主升');
		expect(emotion.tone).toBe('hot');
	});

	it('ranks persistent multi-day leadership above a one-day percentage spike', () => {
		const map = themeMap([
			stock('000001.SZ', '单日涨停股', 10, 250_000_000),
			stock('600001.SH', '持续核心股', 4, 180_000_000),
		], [stock('600001.SH', '持续核心股', 4, 180_000_000)]);
		const histories = {
			'000001.SZ': makeHistory('000001.SZ', [...Array(24).fill(0), 10], 250_000_000),
			'600001.SH': makeHistory('600001.SH', [...Array(20).fill(0), 10, 10, 4, 3, 4], 180_000_000),
		};

		const stocks = buildThemeStocks(map, {}, histories);

		expect(stocks[0].symbol).toBe('600001.SH');
		expect(stocks[0].leader_score).toBeGreaterThan(stocks.find((item) => item.symbol === '000001.SZ')!.leader_score);
		expect(stocks[0].role).toBe('高度龙头候选');
		expect(stocks[0].metrics.max_limit_streak_20d).toBe(2);
		expect(stocks[0].confidence).toBeLessThan(0.85);
		expect(stocks[0].confirmation).not.toBe('已确认');
	});

	it('preserves provider ordering and keeps a provisional theme at rank two', () => {
		const first: ThemeOverview = {
			theme: 'kpl:1', name: '通信', change_percent: 0, main_net_inflow: 0,
			rising_nodes: 0, falling_nodes: 0, matched_nodes: 0, total_nodes: 0,
			top_node_change_percent: 0, trend_score: 70, source_rank: 1,
		};
		const provisional: ThemeOverview = {
			theme: 'compute_rental', name: '算力租赁', change_percent: 0, main_net_inflow: 0,
			rising_nodes: 0, falling_nodes: 0, matched_nodes: 0, total_nodes: 0,
			top_node_change_percent: 0, trend_score: 99, source_rank: 2, provisional: true,
		};
		const third: ThemeOverview = {
			theme: 'kpl:2', name: '芯片', change_percent: 0, main_net_inflow: 0,
			rising_nodes: 0, falling_nodes: 0, matched_nodes: 0, total_nodes: 0,
			top_node_change_percent: 0, trend_score: 90, source_rank: 3,
		};
		expect(rankThemeOverviews([provisional, third, first]).map((item) => item.name)).toEqual(['通信', '算力租赁', '芯片']);
	});

	it('switches Kaipanla ordering between daily and five-day strength', () => {
		const dailyLeader: ThemeOverview = {
			theme: 'kpl:daily', name: '当日爆发', change_percent: 0, main_net_inflow: 0,
			rising_nodes: 0, falling_nodes: 0, matched_nodes: 0, total_nodes: 0,
			top_node_change_percent: 0, trend_score: 80, daily_strength_score: 100, five_day_strength_score: 22,
			source: 'duanxianxia:kaipanla', source_rank: 1,
		};
		const persistent: ThemeOverview = {
			theme: 'kpl:persistent', name: '五日持续', change_percent: 0, main_net_inflow: 0,
			rising_nodes: 0, falling_nodes: 0, matched_nodes: 0, total_nodes: 0,
			top_node_change_percent: 0, trend_score: 70, daily_strength_score: 50, five_day_strength_score: 100,
			source: 'duanxianxia:kaipanla', source_rank: 2,
		};
		expect(rankThemeOverviews([persistent, dailyLeader], 'daily').map((item) => item.name)).toEqual(['当日爆发', '五日持续']);
		expect(rankThemeOverviews([persistent, dailyLeader], 'five_day').map((item) => item.name)).toEqual(['五日持续', '当日爆发']);
		expect(themeStrengthScore(dailyLeader, 'daily')).toBe(100);
		expect(themeStrengthScore(dailyLeader, 'five_day')).toBe(22);
	});

	it('records startup lag so a later low-position stock can follow the supplement path', () => {
		const map = themeMap([
			stock('000001.SZ', '原核心', 1, 300_000_000),
			stock('300001.SZ', '后启动股', 6, 80_000_000),
		]);
		const histories = {
			'000001.SZ': makeHistory('000001.SZ', [...Array(17).fill(0), 10, 5, 3, 2, 1, 1, 1, 1], 300_000_000),
			'300001.SZ': makeHistory('300001.SZ', [...Array(22).fill(0), 1, 2, 6], 80_000_000),
		};

		const stocks = buildThemeStocks(map, {}, histories);
		const later = stocks.find((item) => item.symbol === '300001.SZ')!;

		expect(later.metrics.start_lag_days).toBeGreaterThan(0);
		expect(later.evidence.some((item) => item.includes('补涨路径'))).toBe(true);
	});

	it('elects independent height leaders inside 10cm and 20cm lanes', () => {
		const hayao = {...stock('600664.SH', '哈药股份', 7.9, 4_500_000_000), limit_up_streak: 5, limit_up_days: 5, limit_up_count: 5, limit_regime: '10cm', first_limit_date: '2026-01-20'};
		const wanbang = {...stock('301520.SZ', '万邦医药', 0, 1_100_000_000), limit_up_streak: 1, limit_up_days: 1, limit_up_count: 1, limit_regime: '20cm', first_limit_date: '2026-01-23'};
		const other20 = stock('300001.SZ', '普通20cm跟随', -2, 500_000_000);
		const map = themeMap([hayao, wanbang, other20]);
		const histories = {
			'600664.SH': makeHistory('600664.SH', [...Array(19).fill(0), 10, 10, 10, 10, 10, 7.9], 4_500_000_000),
			'301520.SZ': makeHistory('301520.SZ', [...Array(20).fill(0), 8, 12, 20, 13, 0], 1_100_000_000),
			'300001.SZ': makeHistory('300001.SZ', [...Array(24).fill(0), -2], 500_000_000),
		};

		const stocks = buildThemeStocks(map, {}, histories);
		const tenLeader = stocks.find((item) => item.symbol === '600664.SH')!;
		const twentyLeader = stocks.find((item) => item.symbol === '301520.SZ')!;

		expect(tenLeader.limit_regime).toBe('10cm');
		expect(twentyLeader.limit_regime).toBe('20cm');
		expect(tenLeader.role).toBe('高度龙头候选');
		expect(twentyLeader.role).toBe('高度龙头候选');
	});

	it('keeps full-pool server rank and role when only one page is hydrated', () => {
		const serverLeader = {
			...stock('600010.SH', '全池龙头', -1, 80_000_000),
			rank_score: 91,
			rank_role: '高度龙头候选',
		};
		const localSpike = {
			...stock('300010.SZ', '页内涨幅股', 19.8, 900_000_000),
			rank_score: 46,
			rank_role: '中位跟随',
		};
		const stocks = buildThemeStocks(themeMap([serverLeader, localSpike]), {}, {
			'600010.SH': makeHistory('600010.SH', [...Array(24).fill(0), -1], 80_000_000),
			'300010.SZ': makeHistory('300010.SZ', [...Array(24).fill(0), 19.8], 900_000_000),
		});

		expect(stocks[0].symbol).toBe('600010.SH');
		expect(stocks[0].leader_score).toBe(91);
		expect(stocks[0].role).toBe('高度龙头候选');
		expect(stocks.find((item) => item.symbol === '300010.SZ')?.leader_score).toBe(46);
	});
});

function themeMap(primary: ReturnType<typeof stock>[], secondary: ReturnType<typeof stock>[] = []): SectorMap {
	return {
		theme: 'test', name: '测试题材', tabs: ['测试题材'], meta,
		groups: [{
			id: 'core', name: '核心', nodes: [
				{id: 'a', name: '题材核心', change_percent: 4, main_net_inflow: 1, match_status: 'matched', stocks: primary},
				...(secondary.length ? [{id: 'b', name: '题材分支', change_percent: 3, main_net_inflow: 1, match_status: 'matched', stocks: secondary}] : []),
			],
		}],
	};
}

function stock(symbol: string, name: string, changePercent: number, amount: number) {
	return {
		symbol, name, price: 10, change: changePercent / 10, change_percent: changePercent,
		volume: 1_000_000, amount, total_market_cap: 5_000_000_000,
		float_market_cap: 3_000_000_000, main_net_inflow: amount / 20, meta,
	};
}

function makeHistory(symbol: string, changes: number[], amount: number): KLine[] {
	let close = 10;
	return changes.map((change, index) => {
		const previous = close;
		close = previous * (1 + change / 100);
		const time = new Date(Date.UTC(2026, 0, index + 1)).toISOString();
		return {
			symbol, time, open: previous, high: Math.max(previous, close) * 1.01,
			low: Math.min(previous, close) * 0.99, close, volume: 1_000_000,
			amount, turnover_rate: 5, change_percent: change, meta,
		};
	});
}
