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

	it('preserves the fused daily ranking returned by the backend', () => {
		const first: ThemeOverview = {
			theme: 'kpl:1', name: '通信', change_percent: 0, main_net_inflow: 0,
			rising_nodes: 0, falling_nodes: 0, matched_nodes: 0, total_nodes: 0,
			top_node_change_percent: 0, trend_score: 70, source_rank: 1,
		};
		const industry: ThemeOverview = {
			theme: 'compute_rental', name: '算力租赁', change_percent: 0, main_net_inflow: 0,
			rising_nodes: 0, falling_nodes: 0, matched_nodes: 0, total_nodes: 0,
			top_node_change_percent: 0, trend_score: 99, daily_rank: 2,
		};
		const third: ThemeOverview = {
			theme: 'kpl:2', name: '芯片', change_percent: 0, main_net_inflow: 0,
			rising_nodes: 0, falling_nodes: 0, matched_nodes: 0, total_nodes: 0,
			top_node_change_percent: 0, trend_score: 90, source_rank: 3,
		};
		expect(rankThemeOverviews([industry, third, first]).map((item) => item.name)).toEqual(['通信', '算力租赁', '芯片']);
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

function makeHistory(symbol: string, changes: number[], amount: number, options: { turnover?: number } = {}): KLine[] {
	let close = 10;
	return changes.map((change, index) => {
		const previous = close;
		close = previous * (1 + change / 100);
		const time = new Date(Date.UTC(2026, 0, index + 1)).toISOString();
		// 涨停日按「收盘封死」形态生成（close == high），与真实涨停 K 线一致；
		// 非涨停日保留 1% 上影线。
		const high = change >= 10 ? close : Math.max(previous, close) * 1.01;
		return {
			symbol, time, open: previous, high,
			low: Math.min(previous, close) * 0.99, close, volume: 1_000_000,
			amount, turnover_rate: options.turnover ?? 5, change_percent: change, meta,
		};
	});
}

// 缠论结构维度的语义是「结构是否支持继续领涨」，与 czsc 的多空分不同：
// 前者关心高位动能是否衰竭，后者只回答看多看空。
describe('limit-up event archive', () => {
	it('uses archived event dates over the daily-return approximation', () => {
		const map = themeMap([stock('600001.SH', '事件确认股', 5, 200_000_000)], []);
		// 日K 涨幅达到阈值且封板，但事件库只确认其中一天涨停：以事件为准。
		const histories = {
			'600001.SH': makeHistory('600001.SH', [...Array(18).fill(0), 10, 10], 200_000_000),
		};
		const coverage = new Set(histories['600001.SH'].map((line) => line.time.slice(0, 10)));
		const withArchive = buildThemeStocks(map, {}, histories, {}, {
			'600001.SH': { limitDates: new Set([[...coverage][0]]), coveredDates: new Set([...coverage].slice(0, 18)) },
		});
		const withoutArchive = buildThemeStocks(map, {}, histories);
		// 覆盖窗口不足 5 天：仍视为无档案，回退近似判定为 2 连板。
		expect(withArchive[0].metrics.max_limit_streak_20d).toBe(2);
		expect(withoutArchive[0].metrics.max_limit_streak_20d).toBe(2);

		// 覆盖天数足够时，事件库确认只有 1 天涨停 → 连板高度以档案为准。
		const limitDatesSet = new Set([[...coverage][18]]);
		const fullCoverage = buildThemeStocks(map, {}, histories, {}, {
			'600001.SH': { limitDates: limitDatesSet, coveredDates: coverage },
		});
		expect(fullCoverage[0].metrics.limit_events_covered).toBe(true);
		expect(fullCoverage[0].metrics.max_limit_streak_20d).toBe(1);
		expect(fullCoverage[0].confidence).toBeGreaterThan(withoutArchive[0].confidence);
	});

	it('flags near-limit closes without sealing as non-limit days', () => {
		const map = themeMap([stock('000001.SZ', '大阳未封板', 5, 100_000_000)], []);
		const rises = [...Array(23).fill(0), 9.8, 0];
		const histories = { '000001.SZ': makeHistory('000001.SZ', rises, 100_000_000) };
		const result = buildThemeStocks(map, {}, histories);
		// 9.8% 大阳线带 1% 上影线：涨幅达标但未封板，不计入涨停序列。
		expect(result[0].metrics.max_limit_streak_20d).toBe(0);
	});
});

describe('chan structure dimension', () => {
	it('scores above-pivot uptrend structure higher than below-pivot downtrend', () => {
		const strong = buildThemeStocks(
			themeMap([stock('600001.SH', '结构强势股', 6, 500_000_000)]),
			{},
			{ '600001.SH': makeHistory('600001.SH', [...Array(20).fill(0), 6], 500_000_000) },
			{ '600001.SH': chanAnalysis({ zsState: '中枢上方', biDirection: '向上', progress: 0.4 }) },
		)[0];
		const weak = buildThemeStocks(
			themeMap([stock('600001.SH', '结构弱势股', 6, 500_000_000)]),
			{},
			{ '600001.SH': makeHistory('600001.SH', [...Array(20).fill(0), 6], 500_000_000) },
			{ '600001.SH': chanAnalysis({ zsState: '中枢下方', biDirection: '向下', progress: 0.4 }) },
		)[0];

		expect(strong.structure.available).toBe(true);
		expect(strong.structure.score).toBeGreaterThan(weak.structure.score);
		expect(strong.leader_score).toBeGreaterThan(weak.leader_score);
	});

	it('penalises a top divergence even when the pivot position is still strong', () => {
		const withoutDivergence = buildThemeStocks(
			themeMap([stock('600002.SH', '无背驰', 5, 400_000_000)]),
			{},
			{ '600002.SH': makeHistory('600002.SH', [...Array(20).fill(0), 5], 400_000_000) },
			{ '600002.SH': chanAnalysis({ zsState: '中枢上方', biDirection: '向上', progress: 0.4 }) },
		)[0];
		const withDivergence = buildThemeStocks(
			themeMap([stock('600002.SH', '顶背驰', 5, 400_000_000)]),
			{},
			{ '600002.SH': makeHistory('600002.SH', [...Array(20).fill(0), 5], 400_000_000) },
			{ '600002.SH': chanAnalysis({ zsState: '中枢上方', biDirection: '向上', progress: 0.4, divergence: { kind: '顶背驰', decay: 0.3, price: 12 } }) },
		)[0];

		expect(withDivergence.structure.score).toBeLessThan(withoutDivergence.structure.score);
		expect(withDivergence.structure.reasons.some((item) => item.includes('顶背驰'))).toBe(true);
	});

	it('discounts a late-stage uptrend stroke that is nearly finished', () => {
		const early = buildThemeStocks(
			themeMap([stock('600003.SH', '笔初段', 4, 300_000_000)]),
			{},
			{ '600003.SH': makeHistory('600003.SH', [...Array(20).fill(0), 4], 300_000_000) },
			{ '600003.SH': chanAnalysis({ zsState: '中枢上方', biDirection: '向上', progress: 0.3 }) },
		)[0];
		const late = buildThemeStocks(
			themeMap([stock('600003.SH', '笔末段', 4, 300_000_000)]),
			{},
			{ '600003.SH': makeHistory('600003.SH', [...Array(20).fill(0), 4], 300_000_000) },
			{ '600003.SH': chanAnalysis({ zsState: '中枢上方', biDirection: '向上', progress: 0.95 }) },
		)[0];

		expect(late.structure.score).toBeLessThan(early.structure.score);
		expect(late.structure.reasons.some((item) => item.includes('接近笔终点'))).toBe(true);
	});

	it('leaves the leadership score untouched when no chan result is available', () => {
		const map = themeMap([stock('600004.SH', '无缠论', 5, 300_000_000)]);
		const histories = { '600004.SH': makeHistory('600004.SH', [...Array(20).fill(0), 5], 300_000_000) };
		const withoutChan = buildThemeStocks(map, {}, histories)[0];
		const withNullChan = buildThemeStocks(map, {}, histories, { '600004.SH': null })[0];

		expect(withoutChan.structure.available).toBe(false);
		expect(withoutChan.leader_score).toBe(withNullChan.leader_score);
		expect(withoutChan.breakdown.structure).toBe(0);
	});

	it('adjusts the server-supplied rank score by structure instead of ignoring it', () => {
		const ranked = { ...stock('600010.SH', '全池龙头', 2, 300_000_000), rank_score: 70, rank_role: '核心候选' as const };
		const build = (analysis: ReturnType<typeof chanAnalysis> | null) => buildThemeStocks(
			themeMap([ranked]),
			{},
			{ '600010.SH': makeHistory('600010.SH', [...Array(20).fill(0), 2], 300_000_000) },
			{ '600010.SH': analysis },
		)[0];

		const neutral = build(null);
		expect(neutral.leader_score).toBe(70);

		const supported = build(chanAnalysis({ zsState: '中枢上方', biDirection: '向上', progress: 0.3 }));
		expect(supported.leader_score).toBeGreaterThan(70);

		const pressured = build(chanAnalysis({ zsState: '中枢下方', biDirection: '向下', progress: 0.3 }));
		expect(pressured.leader_score).toBeLessThan(70);
	});

	it('reports availability gaps explicitly instead of a generic warning', () => {
		const complete = buildThemeStocks(
			themeMap([withLimitData(stock('600005.SH', '数据完整', 5, 300_000_000))]),
			{},
			{ '600005.SH': makeHistory('600005.SH', [...Array(20).fill(0), 5], 300_000_000) },
		)[0];
		expect(complete.availability.complete).toBe(true);

		const noTurnover = buildThemeStocks(
			themeMap([withLimitData(stock('600006.SH', '缺换手', 5, 300_000_000))]),
			{},
			{ '600006.SH': makeHistory('600006.SH', [...Array(20).fill(0), 5], 300_000_000, { turnover: 0 }) },
		)[0];
		expect(noTurnover.availability.complete).toBe(false);
		expect(noTurnover.availability.missing).toContain('换手率');
		expect(noTurnover.risks.some((item) => item.includes('换手率') && item.includes('缺失'))).toBe(true);
	});
});

// withLimitData 补上涨停事件字段，让 hasExactLimitData 为真。
function withLimitData<T extends ReturnType<typeof stock>>(item: T) {
	return { ...item, limit_up_streak: 1, limit_up_days: 1, limit_up_count: 1, last_limit_date: '2026-01-05' };
}

// chanAnalysis 构造一份最小可用的缠论结果，只填本组测试关心的结构字段。
function chanAnalysis(options: {
	zsState: string;
	biDirection: string;
	progress: number;
	divergence?: { kind: string; decay: number; price: number };
}) {
	return {
		symbol: '600001.SH',
		freq: '日线',
		generated_at: '2026-01-24T00:00:00Z',
		elapsed_ms: 1200,
		range: { start: '2026-01-01', end: '2026-01-24', bars: 240 },
		structure: {
			counts: { bars: 240, fx: 12, bi: 6, zs: 2 },
			last_close: 12,
			fx: [], bi: [], zs: [],
			current_bi: {
				direction: options.biDirection, start: { time: '2026-01-10', price: 10 },
				end: { time: '2026-01-24', price: 12 }, bars: 8, power: 2, slope: 0.25,
				is_sure: true, progress: options.progress, sure: true,
			},
			zs_position: { state: options.zsState, zone: { start: '', end: '', zg: 11, zd: 10, gg: 11.5, dd: 9.5, amplitude: 15 }, note: '' },
			divergence: options.divergence
				? [{ direction: '向上', kind: options.divergence.kind, time: '2026-01-24', price: options.divergence.price, prev_slope: 0.4, slope: 0.2, decay: options.divergence.decay }]
				: [],
		},
		summary: { score: 55, stance: '中性', tone: 'flat', reasons: [], conclusion: '' },
		signals: [],
	} as unknown as Parameters<typeof buildThemeStocks>[3] extends Record<string, infer V> ? NonNullable<V> : never;
}
